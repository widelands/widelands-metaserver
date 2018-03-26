package relayinterface

// GameData is the data structure passed between client and server over rpc.
type GameData struct {
	Name     string
	Password string
}

/*
Passed Messages:

Precondition: Client is registered on the WLMS and not in any game

WLMS					WLNR			Client
	<-------------- OPEN_GAME (name) -----------------------
 - ServerRPCMethod.NewGame (name, response) -->
 *announce game as closed*
			*open game*
	-------- OPENED_GAME (ip, challenge) ---------------->
		                  <- CONNECT (name, response) -
  <- ClientRPCMethod.GameConnected (name) -

 *announce game as open*
						* setup game *
			...
 <- GAME_STARTED (?) -----------------------------
 *announce game as running*
			...
 <-------------- DISCONNECT ----------------------
 *no longer list game*
			<- DISCONNECT () ---------
			*close game*
 <- ClientRPCMethod.GameClosed (name) -
*/
