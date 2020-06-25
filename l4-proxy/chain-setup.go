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
                log.Printf("%v", actualErr)
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

