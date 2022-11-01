/*
	Copyright (C) 2021-2022  The YNOproject Developers

	This program is free software: you can redistribute it and/or modify
	it under the terms of the GNU Affero General Public License as published by
	the Free Software Foundation, either version 3 of the License, or
	(at your option) any later version.

	This program is distributed in the hope that it will be useful,
	but WITHOUT ANY WARRANTY; without even the implied warranty of
	MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
	GNU Affero General Public License for more details.

	You should have received a copy of the GNU Affero General Public License
	along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package server

import (
	"encoding/json"
	"net/http"
)

func adminGetOnlinePlayers(w http.ResponseWriter, r *http.Request) {
	_, _, rank, _, _, _ := getPlayerDataFromToken(r.Header.Get("Authorization"))
	if rank < 1 {
		handleError(w, r, "access denied")
		return
	}

	var response []PlayerInfo

	clients.Range(func(_, v any) bool {
		client := v.(*SessionClient)

		player := PlayerInfo{
			Uuid: client.uuid,
			Name: client.name,
			Rank: client.rank,
		}

		response = append(response, player)

		return true
	})

	responseJson, err := json.Marshal(response)
	if err != nil {
		handleError(w, r, "error while marshaling")
	}

	w.Write(responseJson)
}

func adminGetBans(w http.ResponseWriter, r *http.Request) {
	_, _, rank, _, _, _ := getPlayerDataFromToken(r.Header.Get("Authorization"))
	if rank < 1 {
		handleError(w, r, "access denied")
		return
	}

	responseJson, err := json.Marshal(getModeratedPlayers(0))
	if err != nil {
		handleError(w, r, "error while marshaling")
	}

	w.Write(responseJson)
}

func adminGetMutes(w http.ResponseWriter, r *http.Request) {
	_, _, rank, _, _, _ := getPlayerDataFromToken(r.Header.Get("Authorization"))
	if rank < 1 {
		handleError(w, r, "access denied")
		return
	}

	responseJson, err := json.Marshal(getModeratedPlayers(1))
	if err != nil {
		handleError(w, r, "error while marshaling")
	}

	w.Write(responseJson)
}

func adminBan(w http.ResponseWriter, r *http.Request) {
	uuid, _, rank, _, _, _ := getPlayerDataFromToken(r.Header.Get("Authorization"))
	if rank < 1 {
		handleError(w, r, "access denied")
		return
	}

	playerParam, ok := r.URL.Query()["player"]
	if !ok || len(playerParam) < 1 {
		handleError(w, r, "player not specified")
		return
	}

	err := tryBanPlayer(uuid, playerParam[0])
	if err != nil {
		handleInternalError(w, r, err)
		return
	}

	w.Write([]byte("ok"))
}

func adminMute(w http.ResponseWriter, r *http.Request) {
	uuid, _, rank, _, _, _ := getPlayerDataFromToken(r.Header.Get("Authorization"))
	if rank < 1 {
		handleError(w, r, "access denied")
		return
	}

	playerParam, ok := r.URL.Query()["player"]
	if !ok || len(playerParam) < 1 {
		handleError(w, r, "player not specified")
		return
	}

	err := tryMutePlayer(uuid, playerParam[0])
	if err != nil {
		handleInternalError(w, r, err)
		return
	}

	w.Write([]byte("ok"))
}

func adminUnban(w http.ResponseWriter, r *http.Request) {
	uuid, _, rank, _, _, _ := getPlayerDataFromToken(r.Header.Get("Authorization"))
	if rank < 1 {
		handleError(w, r, "access denied")
		return
	}

	playerParam, ok := r.URL.Query()["player"]
	if !ok || len(playerParam) < 1 {
		handleError(w, r, "player not specified")
		return
	}

	err := tryUnbanPlayer(uuid, playerParam[0])
	if err != nil {
		handleInternalError(w, r, err)
		return
	}

	w.Write([]byte("ok"))
}

func adminUnmute(w http.ResponseWriter, r *http.Request) {
	uuid, _, rank, _, _, _ := getPlayerDataFromToken(r.Header.Get("Authorization"))
	if rank < 1 {
		handleError(w, r, "access denied")
		return
	}

	playerParam, ok := r.URL.Query()["player"]
	if !ok || len(playerParam) < 1 {
		handleError(w, r, "player not specified")
		return
	}

	err := tryUnmutePlayer(uuid, playerParam[0])
	if err != nil {
		handleInternalError(w, r, err)
		return
	}

	w.Write([]byte("ok"))
}