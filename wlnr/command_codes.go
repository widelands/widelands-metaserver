package main

const (

kRelayProtocolVersion uint8 = 1

// The commands used in the protocol
// The names match the names in the Widelands sources
// Keep this synchronized with relay_protocol.h

// all
kHello uint8 = 1
kWelcome uint8 = 2
kDisconnect uint8 = 3
kPing uint8 = 4
kPong uint8 = 5
// host
kConnectClient uint8 = 11
kDisconnectClient uint8 = 12
kToClients uint8 = 13
kFromClient uint8 = 14
// client
kToHost uint8 = 21
kFromHost uint8 = 22

)
