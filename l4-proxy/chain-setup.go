/* Functions related to setting up a service chain. These can be invoked
 * either by the original client proxy's HTTP control handler, or by
 * a custom P2P protocol handler.
 */

package main

import (
    "bytes"
    "encoding/gob"
    "errors"
    "fmt"
    "io"
    "log"
    "strings"
    "syscall"

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

func NewChainError(err error) *ChainMsg {
    msg := &ChainMsg{Type: Error}
    if err2 := EncodeChainData(msg, err.Error()); err2 != nil {
        log.Printf("ERROR: Unable to encode error message into %s " +
                    "message\n%v\n", Error, err2)
    }

    return msg
}

func NewChainSetupRequest(chainSpec []string) *ChainMsg {
    msg := &ChainMsg{Type: SetupRequest}
    if err := EncodeChainData(msg, chainSpec); err != nil {
        log.Printf("ERROR: Unable to encode chain spec into %s " +
                    "message\n%v\n", SetupRequest, err)
        return nil
    }

    return msg
}

// ACK message doesn't need a payload, but an optional string can be used for debugging
// If not used, simply provide an empty string (i.e. "")
func NewChainSetupACK(debug string) *ChainMsg {
    msg := &ChainMsg{Type: SetupACK}
    if err := EncodeChainData(msg, debug); err != nil {
        // ACK message without debug msg is still valid, so just warn
        log.Printf("WARNING: Unable to encode debug string into %s " +
                    "message\n%v\n", SetupACK, err)
    }

    return msg
}

func NewChainData(data []byte) *ChainMsg {
    msg := &ChainMsg{Type: Data}
    if err := EncodeChainData(msg, data); err != nil {
        log.Printf("ERROR: Unable to encode data into %s " +
                    "message\n%v\n", Data, err)
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
            return fmt.Errorf("ChainMsg type %s can only encode data of " +
                                "type 'error' or 'string'", msg.Type)
        }
    case SetupRequest:
        chainSpec, ok := obj.([]string)
        if !ok {
            return fmt.Errorf("ChainMsg type %s can only encode data of " +
                                "type '[]string'", msg.Type)
        }
        var buf bytes.Buffer
        enc := gob.NewEncoder(&buf)
        enc.Encode(chainSpec)
        msg.Data = buf.Bytes()
    case SetupACK:
        // Expect obj to be type: string
        debugStr, ok := obj.(string)
        if !ok {
            return fmt.Errorf("ChainMsg type %s can only encode data of " +
                                "type 'string'", msg.Type)
        }
        msg.Data = []byte(debugStr)
    case Data:
        dataBytes, ok := obj.([]byte)
        if !ok {
            return fmt.Errorf("ChainMsg type %s can only encode data of " +
                                "type '[]byte'", msg.Type)
        }
        msg.Data = dataBytes
    default:
        return fmt.Errorf("Unknown chain message type: %s\n", msg.Type)
    }

    return nil
}

func DecodeChainData(msg *ChainMsg) (interface{}, error) {
    if msg == nil {
        return nil, fmt.Errorf("ERROR: msg == nil\n")
    }


    switch msg.Type {
    case Error:
        errStr := string(msg.Data)
        return fmt.Errorf(errStr), nil
    case SetupRequest:
        var chainSpec []string
        dec := gob.NewDecoder(bytes.NewBuffer(msg.Data))
        dec.Decode(&chainSpec)
        return chainSpec, nil
    case SetupACK:
        debug := string(msg.Data)
        return debug, nil
    case Data:
        msgBytes := msg.Data
        return msgBytes, nil
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
                                    msgio.NewVarintReaderSize(stream, MAX_UDP_TUNNEL_PAYLOAD))

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
// NOTE: The send* functions are asynchronous (i.e. will send and simply return,
//       and will not wait for a response)
func sendError(cmsr *chainMsgCommunicator, err error) error {
    errMsg := NewChainError(err)
    if err2 := cmsr.Send(errMsg); err2 != nil {
        return fmt.Errorf("Attempt to send %s message failed\n%w\n", errMsg.Type, err2)
    }

    return nil
}

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

func sendData(cmsr *chainMsgCommunicator, data []byte) error {
    dataMsg := NewChainData(data)
    if err := cmsr.Send(dataMsg); err != nil {
        return fmt.Errorf("Attempt to send %s message failed\n%w\n", dataMsg.Type, err)
    }

    return nil
}

func receiveData(cmsr *chainMsgCommunicator) ([]byte, error) {
    msg, err := cmsr.Recv()
    if err != nil {
        return nil, fmt.Errorf("Unable to receive chain message\n%w\n", err);
    }

    if !expectTypePrintErr(msg, Data) {
        return nil, fmt.Errorf("Received ChainMsg was not type %s\n", SetupACK)
    }

    msgData, err := DecodeChainData(msg)
    if err != nil {
        return nil, fmt.Errorf("Unable to decode chain message\n%w\n", err)
    }

    // Type asserting slices returns a pointer to the slice, thus avoiding copies
    msgBytes, ok := msgData.([]byte)
    if !ok {
        return nil, fmt.Errorf("Expected data in %s message to be type '[]byte', " +
                    "but was type '%T'\n", msg.Type, msgData)
    }

    return msgBytes, nil
}

func fwdStream2Stream(src, dst network.Stream) {
    defer func() {
        log.Printf("Closing connection %s <=> %s\n",
            dst.Conn().LocalPeer(), dst.Conn().RemotePeer())
        dst.Reset()
        src.Reset()
    }()

    // Ideally, we can directly forward the connection's bytes. This would
    // be most performant. However, we choose to decode and re-encode to be
    // able to intercept errors that occur and print messages. This is useful
    // for debugging.
    var err error
    var msgBytes []byte
    srcComm := NewChainMsgCommunicator(src)
    dstComm := NewChainMsgCommunicator(dst)

    for {
        if msgBytes, err = receiveData(srcComm); err != nil {
            log.Printf("ERROR: %v\n", err)
            if errors.Is(err, syscall.EINVAL) || errors.Is(err, io.EOF) {
                log.Printf("Connection %s <=> %s closed by remote",
                    src.Conn().LocalPeer(), src.Conn().RemotePeer())
            } else {
                log.Printf("Unable to read from connection %s <=> %s\n%v\n",
                    src.Conn().LocalPeer(), src.Conn().RemotePeer(), err)
            }
            return
        }

        if err = sendData(dstComm, msgBytes); err != nil {
            if errors.Is(err, syscall.EINVAL) {
                log.Printf("Connection %s <=> %s closed",
                    dst.Conn().LocalPeer(), dst.Conn().RemotePeer())
            } else {
                log.Printf("ERROR: Unable to write to connection %s <=> %s\n%v\n",
                    dst.Conn().LocalPeer(), dst.Conn().RemotePeer(), err)
            }
            return
        }
    }
}

// Function for source (client) proxy to begin chain setup operation
func setupChain(chainSpec []string) (network.Stream, error) {
    if len(chainSpec) < 2 {
        return nil, fmt.Errorf("Chain spec must contain at least two tokens " +
            "(transport protocol and a service name)\n")
    }

    var err error

    tpProto := chainSpec[0]
    servName := chainSpec[1]

    // Verify proto
    switch tpProto {
    case "udp", "tcp": // do nothing
    default:
        log.Printf("ERROR: Unrecognized transport protocol: %s\n", tpProto)
        return nil, fmt.Errorf("Expecting protocol to be either 'tcp' or 'udp'\n")
    }

    log.Printf("Requested protocol is: %s\n", tpProto)
    log.Printf("Requested service is: %s\n", servName)

    peerProxyID, err := resolveService(servName)
    if err != nil {
        err = fmt.Errorf("Unable to resolve service %s\n%w\n", servName, err)
        log.Printf("ERROR: %v", err)
        return nil, err
    }

    // Send chain setup request
    var stream network.Stream
    if stream, err = createStream(peerProxyID, chainSetupProtoID); err != nil {
        err = fmt.Errorf("Unable to open stream to peer %s\n%w\n", peerProxyID, err)
        log.Printf("ERROR: %v", err)
        return nil, err
    }

    sendRecv := NewChainMsgCommunicator(stream)
    setupReq := NewChainSetupRequest(chainSpec)
    sendRecv.Send(setupReq)

    // TODO: handleMsg() function that uses type switch and
    //       calls other functions to handle specific types

    // Wait for ack from downstream
    resMsg, err := receiveSetupACK(sendRecv)
    if err != nil {
        err = fmt.Errorf("Unable to receive chain SetupACK\n%w\n", err)
        log.Printf("ERROR: %v\n", err)
        return nil, err
    }

    if resMsg != "" {
        log.Printf("Message received: %s\n", resMsg)
    }

    return stream, nil
}

// Handler for chainSetupProtoID (i.e. invoked at destination proxy)
func chainSetupHandler(stream network.Stream) {
    var err error

    // Input stream sender/receiver
    inSendRecv := NewChainMsgCommunicator(stream)

    chainSpec, err := receiveSetupRequest(inSendRecv)
    if err != nil {
        log.Printf("ERROR: receiveSetupRequest() failed\n%v\n", err)
        return
    }

    // Find ourself (the service this proxy represents) in the chain, what
    // transport protocol we should be using, and the next service in the
    // (if it exists).
    //   - Example chain spec: /tcp/service1/service2/udp/service3
    // TODO: Right now we assume a service is specified ONLY ONCE in the chain
    //       If we allow multiple occurrences, this will need to be re-designed
    // TODO: PREVENT LOOPED CHAIN SPECS!
    var tpProto, tpProtoThis string
    var nextServ string
    foundMe := false
    for _, token := range chainSpec {
        if token == "udp" || token == "tcp" {
            tpProto = token
        } else if token == service { // 'service' is currently global
            foundMe = true
            tpProtoThis = tpProto
        } else if foundMe == true {
            nextServ = token
            break
        }
    }

    if foundMe == false {
        log.Printf("ERROR: This service (%s) was not found in the chain spec: %v\n", service, chainSpec)
        return
    } else if foundMe == true && nextServ == "" {
        // This is the destination service
        // Return msg to previous proxy acknowledging setup
        log.Printf("End of chain reached, sending SetupACK back\n")
        err = sendSetupACK(inSendRecv, REV_CHAIN_MSG_PREFIX + service)
        if err != nil {
            log.Printf("ERROR: Unable to send ACK to previous service\n%v\n", err)
            return
        }

        // Change protocol ID of input stream and invoke proper handler
        if tpProtoThis == "udp" {
            stream.SetProtocol(udpTunnelProtoID)
            udpEndChainHandler(stream)
        } else if tpProtoThis == "tcp" {
            stream.SetProtocol(tcpTunnelProtoID)
            tcpTunnelHandler(stream)
        } else {
            log.Printf("ERROR: Unknown transport protocol\n")
        }

        return
    }

    // If the chain extends beyond this service, keep going.
    // Dial the next service and forward the chain setup message.
    log.Printf("The next service is: %s %s\n", tpProto, nextServ)
    log.Println("Looking for service with name", nextServ, "in hash-lookup")
    peerProxyID, err := resolveService(nextServ)
    if err != nil {
        log.Printf("ERROR: Unable to resolve service %s\n%v\n", nextServ, err)
        return
    }

    // Create output stream sender/receiver to next service
    var outStream network.Stream
    if outStream, err = createStream(peerProxyID, chainSetupProtoID); err != nil {
        log.Printf("ERROR: Unable to dial target peer %s\n%v\n", peerProxyID, err)
        return
    }

    outSendRecv := NewChainMsgCommunicator(outStream)

    // Forward chain setup request
    if err = sendSetupRequest(outSendRecv, chainSpec); err != nil {
        log.Printf("ERROR: sendSetupRequest() failed\n%v\n", err)
        return
    }

    // Wait for ack from downstream
    resMsg, err := receiveSetupACK(outSendRecv)
    if err != nil {
        log.Printf("ERROR: receiveSetupACK() failed\n%v\n", err)
        return
    }

    // NOTE: Passing messages back in the ACK is just for debugging.
    //       Append this service to the ACK data and ACK prev service.
    if strings.HasPrefix(resMsg, REV_CHAIN_MSG_PREFIX) {
        resMsg += " " + service
    }
    sendSetupACK(inSendRecv, resMsg)

    // Change protocol ID of input & output streams and invoke proper handler
    if tpProtoThis == "udp" {
        outStream.SetProtocol(udpTunnelProtoID)
        udpMidChainHandler(stream, outStream)
    } else if tpProtoThis == "tcp" {
        outStream.SetProtocol(tcpTunnelProtoID)
        tcpTunnelHandler(stream)
    } else {
        log.Printf("ERROR: Unknown transport protocol\n")
    }

    return
}
