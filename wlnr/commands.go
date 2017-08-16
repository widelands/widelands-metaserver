package main

const kRelayProtocolVersion uint8 = 1

// The commands used in the protocol
// The names match the names in the Widelands sources
// Keep this synchronized with relay_protocol.h
const kHello uint8 = 1
const kWelcome uint8 = 2
const kDisconnect uint8 = 3
const kConnectClient uint8 = 11
const kDisconnectClient uint8 = 12
const kToClients uint8 = 13
const kFromClient uint8 = 14
const kBroadcast uint8 = 15
const kToHost uint8 = 21
const kFromHost uint8 = 22
