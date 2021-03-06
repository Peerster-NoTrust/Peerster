// Writer for the standard output
package main

import (
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
)

// Strings for messages

func DirectRouteString(origin string, remoteaddr *net.UDPAddr) *string {
	str := fmt.Sprintf("DIRECT-ROUTE FOR %s: %s", origin, addrToString(*remoteaddr))
	return &str
}

func (msg *SimpleMessage) SimpleMessageString() *string {
	str := fmt.Sprintf("CLIENT %s %s", msg.Text, msg.SenderName)
	return &str
}

func (msg *RumorMessage) RumorString(source *net.UDPAddr) *string {
	str := ""
	if msg.Text == "" {
		// route rumor
		str += fmt.Sprintf("DSDV %s: %s:%d \n", msg.Origin, source.IP.String(), source.Port)
	}
	// rumor message
	str += fmt.Sprintf("RUMOR origin %s from %s:%s ID %d contents %s", msg.Origin, source.IP.String(), strconv.Itoa(source.Port), msg.ID, msg.Text)
	return &str
}

func (msg *RumorMessage) MongeringString(dest net.UDPAddr) *string {
	rumorType := "TEXT"
	if msg.isRoute() == true {
		rumorType = "ROUTE"
	} else if msg.isKeyExchange() == true {
		rumorType = "KEY RECORD"
	}
	str := fmt.Sprintf("MONGERING %s with %s:%s", rumorType, dest.IP.String(), strconv.Itoa(dest.Port))
	return &str
}

func (msg *StatusPacket) StatusString(source *net.UDPAddr) *string {
	origins := ""
	for _, peerstatus := range msg.Want {
		origins += fmt.Sprintf(" origin %s nextID %d", peerstatus.Identifier, peerstatus.NextID)
	}
	str := fmt.Sprintf("STATUS from %s:%s%s", source.IP.String(), strconv.Itoa(source.Port), origins)
	return &str
}

func CoinFlipString(dest *net.UDPAddr) *string {
	str := fmt.Sprintf("FLIPPED COIN sending rumor to %s:%s", dest.IP.String(), strconv.Itoa(dest.Port))
	return &str
}

func SyncString(peer *net.UDPAddr) *string {
	str := fmt.Sprintf("IN SYNC WITH %s:%s", peer.IP.String(), strconv.Itoa(peer.Port))
	return &str
}

func (msg *StatusPacket) AntiEntropyString(peer *net.UDPAddr) string {
	origins := ""
	for _, peerstatus := range msg.Want {
		origins += fmt.Sprintf(" origin %s nextID %d", peerstatus.Identifier, peerstatus.NextID)
	}
	return fmt.Sprintf("ANTI ENTROPY STATUS to %s:%s %s",
		peer.IP.String(), strconv.Itoa(peer.Port), origins)
}

func (pm *PrivateMessage) PrivateMessageString(source *net.UDPAddr) *string {
	str := fmt.Sprintf("PRIVATE: %s:%d:%x", pm.Origin, pm.HopLimit, pm.Text)
	return &str
}

///// File Download

func (req *DataRequest) DataRequestString(source *net.UDPAddr) *string {
	str := fmt.Sprintf("DATA REQUEST: %s:%d:%s:%s", req.Origin, req.HopLimit, req.FileName, string(req.HashValue))
	return &str
}

func (reply *DataReply) DataReplyString() *string {
	str := fmt.Sprintf("DATA REPLY: %s:%d:%s:%s", reply.Origin, reply.HopLimit, reply.FileName, hex.EncodeToString(reply.HashValue))
	return &str
}

func FileSubmissionDone(metahash []byte) *string {
	str := fmt.Sprintf("CLIENT FILE ACCEPTED metahash %s", hex.EncodeToString(metahash))
	return &str
}

///// Authentic File Download

func FileWrongSigMetaUploader(uploader string) string {
	str := fmt.Sprintf("WRONG Signature of Metadata by uploader %s \n", uploader)
	str += fmt.Sprintf("WARNING identity of uploader %s cannot be certified", uploader)
	return str
}

func FileGoodSigMetaUploader(uploader string) string {
	return fmt.Sprintf("GOOD Signature of Metadata by uploader %s", uploader)
}

func FileWrongSigUploader(uploader string) string {
	str := fmt.Sprintf("WRONG Signature of File by uploader %s \n", uploader)
	str += fmt.Sprintf("WARNING identity of uploader %s cannot be certified", uploader)
	return str
}

func FileGoodSigUploader(uploader string) string {
	return fmt.Sprintf("GOOD Signature of File by uploader %s", uploader)
}

func FileWrongSigOrigin(origin string) string {
	str := fmt.Sprintf("WRONG Signature of File by Origin %s \n", origin)
	str += fmt.Sprintf("WARNING received file from uncertain origin \n")
	str += fmt.Sprintf("DROPPING file\n")
	return str
}

func FileGoodSigOrigin(origin string) string {
	return fmt.Sprintf("GOOD Signature of File by origin %s", origin)
}

func FileGoodOrigin(origin string) string {
	return fmt.Sprintf("RECEIVED FILE origin %s certified", origin)
}

func FileWarningUnverifiedOrigin() string {
	return fmt.Sprintf("WARNING RECEIVED FILE unverified origin")
}

func FileErrorUnverifiedOrigin(origin string) string {
	return fmt.Sprintf("ERROR RECEIVED FILE unverified origin %s", origin)
}

///// Key Exchange

func KeyExchangeSignString(owner string, sig []byte) string {
	return fmt.Sprintf("SIGNING for %s with sig : \n%s", owner, hex.EncodeToString(sig))
}

func KeyExchangeSendString(owner string, dest net.UDPAddr) string {
	return fmt.Sprintf("KEY EXCHANGE MESSAGE SENT owner %s to %s:%s",
		owner, dest.IP.String(), strconv.Itoa(dest.Port))
}

func KeyExchangeReceiveString(owner string, from net.UDPAddr, valid bool) string {
	str := fmt.Sprintf("KEY EXCHANGE MESSAGE RECEIVED owner %s from %s:%s", owner, from.IP.String(), strconv.Itoa(from.Port))
	if valid {
		str += " VALID"
	} else {
		str += " INVALID"
	}
	return str
}
func KeyExchangeReceiveUnverifiedString(owner, signer string, from net.UDPAddr) string {
	return fmt.Sprintf("KEY EXCHANGE MESSAGE RECEIVED owner %s signed by %s from %s:%s UNVERIFIED", owner, signer, from.IP.String(), strconv.Itoa(from.Port))
}
