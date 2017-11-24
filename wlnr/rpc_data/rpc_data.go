package rpc_data

type NewGameData struct {
	Name     string
	Password string
}

/*
Passed Messages:

Precondition: Client is registered on the WLMS and not in any game

WLMS			WLNR			Client
	<------ OPEN_GAME (name) ------------------
    - OPEN_GAME (name, nonce) -->
 *announce game as closed*
			*open game*
	-------- OPENED_GAME (ip) ---------------->
	                  <- CONNECT (name,nonce) -
  <- GAME_CONNECTED (name) -

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
*/
