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

package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

const (
	delim  = "\uffff"
	mdelim = "\ufffe"
)

var (
	delimBytes = []byte("\uffff")

	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	rooms = make(map[int]*Room)
)

// Room maintains the set of active clients and broadcasts messages to the
// clients.
type Room struct {
	// Registered clients.
	clients sync.Map

	roomId       int
	singleplayer bool

	conditions []*Condition

	minigameConfigs []*MinigameConfig
}

func createRooms(roomIds []int, spRooms []int) {
	for _, roomId := range roomIds {
		rooms[roomId] = &Room{
			roomId:          roomId,
			singleplayer:    contains(spRooms, roomId),
			conditions:      getRoomConditions(roomId),
			minigameConfigs: getRoomMinigameConfigs(roomId),
		}
	}
}

func handleRoom(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, http.Header{"Sec-Websocket-Protocol": {r.Header.Get("Sec-Websocket-Protocol")}})
	if err != nil {
		log.Println(err)
		return
	}

	id, ok := r.URL.Query()["id"]
	if !ok {
		return
	}

	idInt, err := strconv.Atoi(id[0])
	if err != nil {
		log.Println(err)
		return
	}

	var playerToken string
	if token, ok := r.URL.Query()["token"]; ok && len(token[0]) == 32 {
		playerToken = token[0]
	}

	if room, ok := rooms[idInt]; ok {
		room.addClient(conn, getIp(r), playerToken)
	}
}

func (r *Room) addClient(conn *websocket.Conn, ip string, token string) {
	client := &RoomClient{
		room:        r,
		conn:        conn,
		writerEnd:   make(chan bool, 1),
		send:        make(chan []byte, 16),
		receive:     make(chan []byte, 16),
		key:         generateKey(),
		pictures:    make(map[int]*Picture),
		mapId:       fmt.Sprintf("%04d", r.roomId),
		switchCache: make(map[int]bool),
		varCache:    make(map[int]int),
	}

	var uuid string
	if token != "" {
		uuid = getUuidFromToken(token)
	}

	if uuid == "" {
		uuid, _, _ = getOrCreatePlayerData(ip)
	}

	if s, ok := clients.Load(uuid); ok {
		session := s.(*SessionClient)
		if session.hClient != nil {
			writeErrLog(ip, strconv.Itoa(r.roomId), "session in use")
			return
		}

		session.hClient = client
		client.sClient = session
	} else {
		writeErrLog(ip, strconv.Itoa(r.roomId), "player has no session")
		return
	}

	if tags, err := getPlayerTags(uuid); err != nil {
		writeErrLog(ip, strconv.Itoa(r.roomId), "failed to read player tags")
	} else {
		client.tags = tags
	}

	// register client to the room
	r.clients.Store(client, nil)

	// queue s message
	client.sendMsg("s", client.sClient.id, int(client.key), uuid, client.sClient.rank, client.sClient.account, client.sClient.badge) // "your id is %id%" message

	go client.msgProcessor()

	go client.msgWriter()
	go client.msgReader()

	writeLog(ip, strconv.Itoa(r.roomId), "connect", 200)
}

func (r *Room) broadcast(sender *RoomClient, segments ...any) {
	if r.singleplayer {
		return
	}

	r.clients.Range(func(k, _ any) bool {
		client := k.(*RoomClient)
		if !client.valid || (client == sender && segments[0].(string) != "say") {
			return true
		}

		client.sendMsg(segments...)

		return true
	})
}

func (r *Room) processMsgs(sender *RoomClient, data []byte) (errs []error) {
	if len(data) < 8 || len(data) > 4096 {
		return append(errs, errors.New("bad request size"))
	}

	if !verifySignature(sender.key, data) {
		return append(errs, errors.New("bad signature"))
	}

	if !verifyCounter(&sender.counter, data) {
		return append(errs, errors.New("bad counter"))
	}

	data = data[8:]

	for _, v := range data {
		if v < 32 {
			return append(errs, errors.New("bad byte sequence"))
		}
	}

	if !utf8.Valid(data) {
		return append(errs, errors.New("invalid UTF-8"))
	}

	// message processing
	for _, msgStr := range strings.Split(string(data), mdelim) {
		err := r.processMsg(msgStr, sender)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

func (r *Room) processMsg(msgStr string, sender *RoomClient) (err error) {
	msgFields := strings.Split(msgStr, delim)

	if len(msgFields) == 0 {
		return err
	}

	if !sender.valid {
		if msgFields[0] == "ident" {
			err = r.handleIdent(msgFields, sender)
		}
	} else {
		switch msgFields[0] {
		case "m", "tp": // moved / teleported to x y
			err = r.handleM(msgFields, sender)
		case "f": // change facing direction
			err = r.handleF(msgFields, sender)
		case "spd": // change my speed to spd
			err = r.handleSpd(msgFields, sender)
		case "spr": // change my sprite
			err = r.handleSpr(msgFields, sender)
		case "fl", "rfl": // player flash / repeating player flash
			err = r.handleFl(msgFields, sender)
		case "rrfl": // remove repeating player flash
			err = r.handleRrfl(sender)
		case "h": // change sprite visibility
			err = r.handleH(msgFields, sender)
		case "sys": // change my system graphic
			err = r.handleSys(msgFields, sender)
		case "se": // play sound effect
			err = r.handleSe(msgFields, sender)
		case "ap", "mp": // add picture / move picture
			err = r.handleP(msgFields, sender)
		case "rp": // remove picture
			err = r.handleRp(msgFields, sender)
		case "say":
			err = r.handleSay(msgFields, sender)
		case "ss": // sync switch
			err = r.handleSs(msgFields, sender)
		case "sv": // sync variable
			err = r.handleSv(msgFields, sender)
		case "sev":
			err = r.handleSev(msgFields, sender)
		default:
			err = errors.New("unknown message type")
		}
	}
	if err != nil {
		return err
	}

	writeLog(sender.sClient.ip, strconv.Itoa(r.roomId), msgStr, 200)

	return nil
}

func (r *Room) handleValidClient(client *RoomClient) {
	if !r.singleplayer {
		// tell everyone that a new client has connected
		r.broadcast(client, "c", client.sClient.id, client.sClient.uuid, client.sClient.rank, client.sClient.account, client.sClient.badge) // user %id% has connected message

		// send name of client
		if client.sClient.name != "" {
			r.broadcast(client, "name", client.sClient.id, client.sClient.name)
		}

		// send the new client info about the game state
		r.clients.Range(func(k, _ any) bool {
			otherClient := k.(*RoomClient)
			if !otherClient.valid || otherClient == client {
				return true
			}

			client.sendMsg("c", otherClient.sClient.id, otherClient.sClient.uuid, otherClient.sClient.rank, otherClient.sClient.account, otherClient.sClient.badge)
			client.sendMsg("m", otherClient.sClient.id, otherClient.x, otherClient.y)
			if otherClient.facing > 0 {
				client.sendMsg("f", otherClient.sClient.id, otherClient.facing)
			}
			client.sendMsg("spd", otherClient.sClient.id, otherClient.spd)
			if otherClient.sClient.name != "" {
				client.sendMsg("name", otherClient.sClient.id, otherClient.sClient.name)
			}
			if otherClient.sClient.spriteIndex >= 0 { // if the other client sent us valid sprite and index before
				client.sendMsg("spr", otherClient.sClient.id, otherClient.sClient.spriteName, otherClient.sClient.spriteIndex)
			}
			if otherClient.repeatingFlash {
				client.sendMsg("rfl", otherClient.sClient.id, otherClient.flash[:])
			}
			if otherClient.hidden {
				client.sendMsg("h", otherClient.sClient.id, "1")
			}
			if otherClient.sClient.systemName != "" {
				client.sendMsg("sys", otherClient.sClient.id, otherClient.sClient.systemName)
			}
			for picId, pic := range otherClient.pictures {
				client.sendMsg("ap", otherClient.sClient.id, picId, pic.positionX, pic.positionY, pic.mapX, pic.mapY, pic.panX, pic.panY, pic.magnify, pic.topTrans, pic.bottomTrans, pic.red, pic.blue, pic.green, pic.saturation, pic.effectMode, pic.effectPower, pic.name, pic.useTransparentColor, pic.fixedToMap)
			}

			return true
		})
	}

	checkRoomConditions(r, client, "", "")

	for _, minigame := range r.minigameConfigs {
		score, err := getPlayerMinigameScore(client.sClient.uuid, minigame.MinigameId)
		if err != nil {
			writeErrLog(client.sClient.ip, strconv.Itoa(r.roomId), "failed to read player minigame score for "+minigame.MinigameId)
		}
		client.minigameScores = append(client.minigameScores, score)
		varSyncType := 1
		if minigame.InitialVarSync {
			varSyncType = 2
		}
		client.sendMsg("sv", minigame.VarId, varSyncType)
	}

	// send variable sync request for vending machine expeditions
	if r.roomId != currentEventVmMapId {
		return
	}

	if eventIds, hasVms := eventVms[r.roomId]; hasVms {
		for _, eventId := range eventIds {
			if eventId != currentEventVmEventId {
				continue
			}
			client.sendMsg("sev", eventId, "1")
		}
	}
}