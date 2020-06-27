/* Functions related to setting up a service chain. These can be invoked
 * either by the original client proxy's HTTP control handler, or by
 * a custom P2P protocol handler.
 */

package main

import (
    "bytes"
    "encoding/gob"
    "fmt"
    "log"

    "github.com/libp2p/go-msgio"
    "github.com/libp2p/go-libp2p-core/network"
    "github.com/libp2p/go-libp2p-core/protocol"

)

var chainSetupProtoID = protocol.ID("/ChainSetup/1.0")

// Service chain messaging protocol objects
// Enumerate chain message types
type ChainMsgType int
const (
    Error ChainMsgType = iota
    SetupRequest
    SetupACK
    Data
)

const REV_CHAIN_MSG_PREFIX = "Reverse chain: "

func (cmType ChainMsgType) String() string {
    switch cmType {
    case Error:
        return "Error"
    case SetupRequest:
        return "SetupRequest"
    case SetupACK:
        return "SetupACK"
    case Data:
        return "Data"
    default:
        return fmt.Sprintf("%d", cmType)
    }
}

type ChainMsg struct {
    Type    ChainMsgType

    // Arbitrary data that will need further decoding, depending on Type
    Data    []byte
}

func NewChainSetupRequest(chainSpec []string) *ChainMsg {
    msg := &ChainMsg{Type: SetupRequest}
    if err := EncodeChainData(msg, chainSpec); err != nil {
        log.Printf("ERROR: Unable to encode chain spec into SetupRequest message\n%v", err)
        return nil
    }

    return msg
}

// ACK message doesn't need a payload, but an optional string can be used for debugging
// If not used, simply provide an empty string (i.e. "")
func NewChainSetupACK(debug string) *ChainMsg {
    msg := &ChainMsg{Type: SetupACK}
    if len(debug) > 0 {
        if err := EncodeChainData(msg, debug); err != nil {
            // ACK message without debug msg is still valid, so just warn
            log.Printf("WARNING: Unable to encode debug string into SetupACK message\n")
        }
    }

    return msg
}

// Used for setting the Data field of ChainMsg
func EncodeChainData(msg *ChainMsg, obj interface{}) error {
    switch msg.Type {
    case Error:
        // Expect obj to be type: error
        if errObj, ok := obj.(error); ok {
            msg.Data = []byte(errObj.Error())
        } else if str, ok := obj.(string); ok {
            msg.Data = []byte(str)
        } else {
            return fmt.Errorf("ChainMsg type %s can only encode data of type 'error' or 'string'")
        }
    case SetupRequest:
        chainSpec, ok := obj.([]string)
        if !ok {
            return fmt.Errorf("ChainMsg type %s can only encode data of type '[]string'")
        }
        var buf bytes.Buffer
        enc := gob.NewEncoder(&buf)
        enc.Encode(chainSpec)
        msg.Data = buf.Bytes()
    case SetupACK:
        // Expect obj to be type: string
        debugStr, ok := obj.(string);
        if !ok {
            return fmt.Errorf("ChainMsg type %s can only encode data of type 'string'")
        }
        msg.Data = []byte(debugStr)
    case Data:
        // TODO
        fmt.Printf("TO DO\n")
    default:
        return fmt.Errorf("Unknown chain message type: %s\n")
    }

    return nil
}

func DecodeChainData(msg *ChainMsg) (interface{}, error) {
    if msg == nil {
        return nil, fmt.Errorf("ERROR: msg == nil\n")
    }

    buf := bytes.NewBuffer(msg.Data)
    dec := gob.NewDecoder(buf)

    switch msg.Type {
    case Error:
        errStr := string(msg.Data)
        return fmt.Errorf(errStr), nil
    case SetupRequest:
        var chainSpec []string
        dec.Decode(&chainSpec)
        return chainSpec, nil
    case SetupACK:
        debug := string(msg.Data)
        return debug, nil
    case Data:
        // TODO
        return nil, nil
    default:
        return nil, fmt.Errorf("ERROR: Unknown chain message type: %d\n")
    }

    return nil, fmt.Errorf("ERROR: Should not reach this point... " +
        "unless some message types are unimplemented\n")
}

func expectTypePrintErr(cm *ChainMsg, ct ChainMsgType) bool {
    if cm.Type != ct {
        if cm.Type == Error {
            // Print the error message
            actualErr, err := DecodeChainData(cm)
            if err != nil {
                log.Printf("ERROR: Unable to decode chain message: %v\n", err)
            } else {
                log.Printf("ChainMsg ERROR: %v\n", actualErr)
            }
        } else {
            log.Printf("ERROR: Expected a chain type %s, but got %s instead\n", ct, cm.Type)
        }
        return false
    }

    return true
}

// Wrappers for sending/receiving objects through the p2p stream
// msgio is used for framing, while gob is used for encoding/decoding objects
type chainMsgCommunicator struct {
    stream  network.Stream
    msgRWC  msgio.ReadWriteCloser
    enc     *gob.Encoder
    dec     *gob.Decoder
}

func NewChainMsgCommunicator (stream network.Stream) *chainMsgCommunicator {
    if stream == nil {
        return nil
    }

    cmComm := chainMsgCommunicator{stream: stream,}

    // Use msgio for proper framing
    // Keep separate handles for each so we can close independently
    cmComm.msgRWC = msgio.Combine(msgio.NewVarintWriter(stream),
                                    msgio.NewVarintReader(stream))

    // Use gob for encoding/decoding
    cmComm.dec = gob.NewDecoder(cmComm.msgRWC)
    cmComm.enc = gob.NewEncoder(cmComm.msgRWC)

    return &cmComm
}

func (cmsr *chainMsgCommunicator) Recv() (*ChainMsg, error) {
    cm := &ChainMsg{}
    if err := cmsr.dec.Decode(cm); err != nil {
        return nil, err
    }
    return cm, nil
}

func (cmsr *chainMsgCommunicator) Send(obj *ChainMsg) error {
    return cmsr.enc.Encode(obj)
}

func (cmsr *chainMsgCommunicator) GetStream() network.Stream {
    return cmsr.stream
}

// TODO: Wrap the following functions in a "NFV" structure?
//       It'd be responsible for managing the 3 connections (input/output/service)
// NOTE: This function is asynchronous (i.e. will send and simply return, will
//       not wait for a SetupACK)
func sendSetupRequest(cmsr *chainMsgCommunicator, chainSpec []string) error {
    setupMsg := NewChainSetupRequest(chainSpec)
    if err := cmsr.Send(setupMsg); err != nil {
        return fmt.Errorf("Attempt to send %s message failed\n%w\n", setupMsg.Type, err)
    }

    return nil
}

func receiveSetupRequest(cmsr *chainMsgCommunicator) ([]string, error) {
    msg, err := cmsr.Recv()
    if err != nil {
        return nil, fmt.Errorf("Unable to receive chain message\n%w\n", err);
    }

    if !expectTypePrintErr(msg, SetupRequest) {
        return nil, fmt.Errorf("Received ChainMsg was not type %s\n", SetupRequest)
    }

    msgData, err := DecodeChainData(msg)
    if err != nil {
        return nil, fmt.Errorf("Unable to decode chain message\n%w\n", err)
    }

    chainSpec, ok := msgData.([]string)
    if !ok {
        return nil, fmt.Errorf("Expected data in %s message to be type '[]string', " +
                    "but was type '%T'\n", msg.Type, msgData)
    }

    return chainSpec, nil
}

// ACK message doesn't need a payload, but an optional string can be used for debugging
// If 'debug' is not used, simply provide an empty string (i.e. "")
func sendSetupACK(cmsr *chainMsgCommunicator, debug string) error {
    resMsg := NewChainSetupACK(debug)
    if err := cmsr.Send(resMsg); err != nil {
        return fmt.Errorf("Attempt to send %s message failed\n%w\n", resMsg.Type, err)
    }

    return nil
}

// Waits to receive a ChainMsg and verifies it is of type SetupACK.
// Returns the ChainMsg's Data as a string, if it exists. Note that
// the output string is just for any debugging data to be passed back.
func receiveSetupACK(cmsr *chainMsgCommunicator) (string, error) {
    msg, err := cmsr.Recv()
    if err != nil {
        return "", fmt.Errorf("Unable to receive chain message\n%w\n", err);
    }

    if !expectTypePrintErr(msg, SetupACK) {
        return "", fmt.Errorf("Received ChainMsg was not type %s\n", SetupACK)
    }

    // NOTE: Passing messages back in the ACK is just for debugging.
    //       Receiving the ACK alone should indicate success.
    msgData, err := DecodeChainData(msg)
    if err != nil {
        return "", fmt.Errorf("Unable to decode chain message\n%w\n", err)
    }

    debugStr, ok := msgData.(string)
    if !ok {
        return "", fmt.Errorf("Expected data in %s message to be type '[]string', " +
                    "but was type '%T'\n", msg.Type, msgData)
    }

    return debugStr, nil
}

