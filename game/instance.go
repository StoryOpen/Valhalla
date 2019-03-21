package game

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/Hucaru/Valhalla/game/def"
	"github.com/Hucaru/Valhalla/game/packet"
	"github.com/Hucaru/Valhalla/mnet"
	"github.com/Hucaru/Valhalla/mpacket"
	"github.com/Hucaru/Valhalla/nx"
)

type Instance struct {
	mapID                int32
	npcs                 []def.NPC
	mobs                 []gameMob
	players              []mnet.MConnChannel
	workDispatch         chan func()
	previousMobSpawnTime int64
	mapData              nx.Map
}

func createInstanceFromMapData(mapData nx.Map, mapID int32, dispatcher chan func()) *Instance {
	npcs := []def.NPC{}
	mobs := []gameMob{}

	for _, l := range mapData.Mobs {
		nxMob, err := nx.GetMob(l.ID)

		if err != nil {
			continue
		}

		mobs = append(mobs, createNewMob(int32(len(mobs)+1), mapID, l, nxMob))
	}

	for _, l := range mapData.NPCs {
		npcs = append(npcs, def.CreateNPC(int32(len(npcs)), l))
	}

	inst := &Instance{mapID: mapID, npcs: npcs, mobs: mobs, workDispatch: dispatcher, mapData: mapData}

	// Periodic map work
	go func(inst *Instance) {
		timer := time.NewTicker(1000 * time.Millisecond)
		quit := make(chan bool)

		for {
			select {
			case <-timer.C:
				inst.workDispatch <- func() {
					if inst == nil {
						quit <- true
					} else {
						inst.periodicWork()
					}
				}
			case <-quit:
				return
			}
		}

	}(inst)

	return inst
}

func (inst *Instance) send(p mpacket.Packet) {
	for _, v := range inst.players {
		v.Send(p)
	}
}

func (inst *Instance) sendExcept(p mpacket.Packet, exception mnet.MConnChannel) {
	for _, v := range inst.players {
		if v == exception {
			continue
		}

		v.Send(p)
	}
}

func (inst *Instance) addPlayer(conn mnet.MConnChannel) {
	for i, mob := range inst.mobs {
		if mob.HP > 0 {
			mob.SummonType = -1 // -2: fade in spawn animation, -1: no spawn animation
			conn.Send(packet.MobShow(mob.Mob))

			if mob.Controller == nil {
				inst.mobs[i].ChangeController(conn)
			}
		}
	}

	for _, npc := range inst.npcs {
		conn.Send(packet.NpcShow(npc))
		conn.Send(packet.NPCSetController(npc.SpawnID, true))
	}

	player := Players[conn]

	for _, other := range inst.players {
		otherPlayer := Players[other]
		otherPlayer.Send(packet.MapPlayerEnter(player.Char()))

		player.Send(packet.MapPlayerEnter(otherPlayer.Char()))

		if otherPlayer.RoomID > 0 {
			r := Rooms[otherPlayer.RoomID]

			switch r.(type) {
			case *OmokRoom:
				omokRoom := r.(*OmokRoom)

				if omokRoom.IsOwner(other) {
					player.Send(packet.MapShowGameBox(otherPlayer.Char().ID, omokRoom.ID, byte(omokRoom.RoomType), omokRoom.BoardType, omokRoom.Name, bool(len(omokRoom.Password) > 0), omokRoom.InProgress, 0x01))
				}
			case *MemoryRoom:
				memoryRoom := r.(*MemoryRoom)
				if memoryRoom.IsOwner(other) {
					player.Send(packet.MapShowGameBox(otherPlayer.Char().ID, memoryRoom.ID, byte(memoryRoom.RoomType), memoryRoom.BoardType, memoryRoom.Name, bool(len(memoryRoom.Password) > 0), memoryRoom.InProgress, 0x01))
				}
			}
		}
	}

	inst.players = append(inst.players, conn)
}

func (inst *Instance) removePlayer(conn mnet.MConnChannel) {
	ind := -1
	for i, v := range inst.players {
		if v == conn {
			ind = i
		}
	}

	if ind == -1 {
		return // This should not be possible
	}

	inst.players = append(inst.players[:ind], inst.players[ind+1:]...)

	for i, v := range inst.mobs {
		if v.Controller == conn {
			inst.mobs[i].ChangeController(inst.findController())
		}
	}

	player := Players[conn]

	for _, other := range inst.players {
		other.Send(packet.MapPlayerLeft(player.Char().ID))
	}
}

func (inst *Instance) findController() mnet.MConnChannel {
	for _, p := range inst.players {
		return p
	}

	return nil
}

func (inst *Instance) findControllerExcept(conn mnet.MConnChannel) mnet.MConnChannel {
	for _, p := range inst.players {
		if p == conn {
			continue
		}

		return p
	}

	return nil
}

func (inst *Instance) generateMobSpawnID() int32 {
	var l int32
	for _, v := range inst.mobs {
		if v.SpawnID > l {
			l = v.SpawnID
		}
	}

	l++

	if l == 0 {
		l++
	}

	return l
}

func (inst *Instance) SpawnMob(mobID, spawnID int32, x, y, foothold int16, summonType int8, summonOption int32, facesLeft bool) {
	m, err := nx.GetMob(mobID)

	if err != nil {
		return
	}

	mob := createNewMob(spawnID, inst.mapID, nx.Life{}, m)
	mob.ID = mobID

	mob.X = x
	mob.Y = y
	mob.Foothold = foothold

	mob.Respawns = false

	mob.SummonType = summonType
	mob.SummonOption = summonOption

	mob.FaceLeft = facesLeft

	inst.send(packet.MobShow(mob.Mob))

	if summonType != -4 {
		mob.SummonType = -1
		mob.SummonOption = 0
	}

	inst.mobs = append(inst.mobs, mob)

	inst.mobs[len(inst.mobs)-1].ChangeController(inst.findController())
}

func (inst *Instance) SpawnMobNoRespawn(mobID, spawnID int32, x, y, foothold int16, summonType int8, summonOption int32, facesLeft bool) {
	m, err := nx.GetMob(mobID)

	if err != nil {
		return
	}

	mob := def.CreateMob(spawnID, nx.Life{}, m, nil)
	mob.ID = mobID

	mob.X = x
	mob.Y = y
	mob.Foothold = foothold

	mob.Respawns = false

	mob.SummonType = summonType
	mob.SummonOption = summonOption

	mob.FaceLeft = facesLeft

	inst.send(packet.MobShow(mob))

	if summonType != -4 {
		mob.SummonType = -1
		mob.SummonOption = 0
	}

	inst.mobs = append(inst.mobs, gameMob{Mob: mob, mapID: inst.mapID})

	inst.mobs[len(inst.mobs)-1].Controller = inst.findController()
}

func (inst *Instance) handleDeadMobs() {
	y := inst.mobs[:0]

	for _, mob := range inst.mobs {
		if mob.HP < 1 {
			mob.Controller.Send(packet.MobEndControl(mob.Mob))

			for _, id := range mob.Revives {
				inst.SpawnMobNoRespawn(id, inst.generateMobSpawnID(), mob.X, mob.Y, mob.Foothold, -3, mob.SpawnID, mob.FacesLeft())
				y = append(y, inst.mobs[len(inst.mobs)-1])
			}

			if mob.Exp > 0 {
				for player, _ := range mob.dmgTaken {
					p, err := Players.GetFromConn(player)

					if err != nil {
						continue
					}

					// perform exp calculation

					p.GiveEXP(int32(mob.Exp), true, false)
				}
			}

			inst.send(packet.MobRemove(mob.Mob, 1)) // 0 keeps it there and is no longer attackable, 1 normal death, 2 disaapear instantly
		} else {
			y = append(y, mob)
		}
	}

	inst.mobs = y
}

func (inst *Instance) capacity() int {
	// if no mob capacity limit flag present return current mob count

	if len(inst.players) > (inst.mapData.MobCapacityMin / 2) {
		if len(inst.players) < (inst.mapData.MobCapacityMin * 2) {
			return inst.mapData.MobCapacityMin + (inst.mapData.MobCapacityMax-inst.mapData.MobCapacityMin)*(2*len(inst.players)-inst.mapData.MobCapacityMin)/(3*inst.mapData.MobCapacityMin)
		}

		return inst.mapData.MobCapacityMax
	}

	return inst.mapData.MobCapacityMin
}

func (inst *Instance) handleMobRespawns(currentTime int64) {
	if currentTime-inst.previousMobSpawnTime < 7000 {
		return
	}

	inst.previousMobSpawnTime = currentTime

	capacity := inst.capacity()

	if capacity < 0 {
		return
	}

	amountCanSpawn := capacity - len(inst.mobs)

	if amountCanSpawn < 1 {
		return
	}

	mobsToSpawn := []nx.Life{}

	for _, mob := range inst.mapData.Mobs {
		addInfront := true
		regenInterval := mob.MobTime

		if regenInterval == 0 { // Standard mobs
			anyMobSpawned := len(inst.mobs) != 0

			if anyMobSpawned {
				rect := nx.Rectangle{int(mob.X - 100), int(mob.Y - 100), int(mob.X + 100), int(mob.Y + 100)}
				for _, currentMob := range inst.mobs {
					if !rect.Contains(int(currentMob.X), int(currentMob.Y)) {
						continue
					}
				}
			} else {
				addInfront = false
			}
		} else if regenInterval < 0 { // ?
			fmt.Println("Hit less than zero regen interval for", mob)
			// if not reset continue
		} else { // Boss mobs
			// if mob count != 0 continue
			// if current time - mob time can regen < 0 continue
		}

		if addInfront {
			mobsToSpawn = append([]nx.Life{mob}, mobsToSpawn...)
		} else {
			mobsToSpawn = append(mobsToSpawn, mob)
		}
	}

	for len(mobsToSpawn) > 0 && amountCanSpawn > 0 {
		mob := mobsToSpawn[0]

		if mob.MobTime == 0 {
			ind := rand.Intn(len(mobsToSpawn))
			mob = mobsToSpawn[ind]
			mobsToSpawn = append(mobsToSpawn[:ind], mobsToSpawn[ind+1:]...)
		}

		inst.SpawnMob(mob.ID, inst.generateMobSpawnID(), mob.X, mob.Y, mob.Foothold, -2, 0, mob.FaceLeft)
		amountCanSpawn--
	}
}

func (inst *Instance) periodicWork() {
	currentTime := time.Now().UnixNano() / int64(time.Millisecond)
	// Update drops
	// Update mist
	// update portals

	if len(inst.players) > 0 {
		inst.handleMobRespawns(currentTime)
		// check vac hack

		// for each character
		// tick for map dmg e.g. drowning
		// if pet present perform duties
	}
}
