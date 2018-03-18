package player

import (
	"log"

	"github.com/Hucaru/Valhalla/constants"
	"github.com/Hucaru/Valhalla/message"

	"github.com/Hucaru/Valhalla/character"
	"github.com/Hucaru/Valhalla/connection"
	"github.com/Hucaru/Valhalla/interfaces"
	"github.com/Hucaru/Valhalla/movement"
	"github.com/Hucaru/gopacket"
)

func HandleConnect(conn interfaces.ClientConn, reader gopacket.Reader) uint32 {
	charID := reader.ReadUint32()

	char := character.GetCharacter(charID)
	char.SetEquips(character.GetCharacterEquips(char.GetCharID()))
	char.SetSkills(character.GetCharacterSkills(char.GetCharID()))
	char.SetItems(character.GetCharacterItems(char.GetCharID()))

	var isAdmin bool

	err := connection.Db.QueryRow("SELECT isAdmin from users where userID=?", char.GetUserID()).Scan(&isAdmin)

	if err != nil {
		panic(err)
	}

	channelID := uint32(0) // Either get from world server or have it be part of config file

	conn.SetAdmin(isAdmin)
	conn.SetIsLogedIn(true)
	conn.SetChanID(channelID)

	charsPtr.AddOnlineCharacter(conn, &char)

	conn.Write(enterGame(char, channelID))

	conn.Write(message.ScrollingHeaderPacket(constants.GetHeader()))

	log.Println(char.GetName(), "has loged in from", conn)

	return char.GetCurrentMap()
}

func HandleRequestAvatarInfoWindow(conn interfaces.ClientConn, reader gopacket.Reader) {
	charID := reader.ReadUint32()
	char := charsPtr.GetCharFromID(charID)

	if char == nil {
		return
	}

	handle := charsPtr.GetConnHandleFromName(char.GetName())

	conn.Write(avatarSummaryWindow(charID, char, handle))
}

func HandleMovement(conn interfaces.ClientConn, reader gopacket.Reader) (uint32, gopacket.Packet) {
	reader.ReadBytes(5) // used in movement validation
	char := charsPtr.GetOnlineCharacterHandle(conn)

	nFrags := reader.ReadByte()

	movement.ParseFragments(nFrags, char, reader)

	return char.GetCurrentMap(), playerMovePacket(char.GetCharID(), reader.GetBuffer()[2:])
}

func HandleTakeDamage(conn interfaces.ClientConn, reader gopacket.Reader) (uint32, gopacket.Packet) {
	char := charsPtr.GetOnlineCharacterHandle(conn)

	dmgType := reader.ReadByte()
	ammount := reader.ReadUint32()

	mobID := uint32(0)
	reduction := byte(0)
	stance := byte(0)
	hit := byte(0)

	switch dmgType {
	case 0xFE: // map or fall damage
	default:
		mobID = reader.ReadUint32()
		reader.ReadUint32() // some form of map object id?
		hit = reader.ReadByte()
		reduction = reader.ReadByte()
		stance = reader.ReadByte()
	}

	// Handle character buffs e.g. magic guard, power guard etc

	// Modify character hp

	return char.GetCurrentMap(), receivedDmgPacket(char.GetCharID(), ammount, dmgType, mobID, hit, reduction, stance)
}

func HandlePassiveRegen(conn interfaces.ClientConn, reader gopacket.Reader) {
	reader.ReadBytes(4) //?

	hp := reader.ReadUint16()
	mp := reader.ReadUint16()

	char := charsPtr.GetOnlineCharacterHandle(conn)

	if char.GetHP() == 0 || hp > 400 || mp > 1000 || (hp > 0 && mp > 0) {
		return
	}

	if hp > 0 {
		char.SetHP(char.GetHP() + hp)

		if char.GetHP() > char.GetMaxHP() {
			char.SetHP(char.GetMaxHP())
		}

		conn.Write(statChangePacket(true, hpID, uint32(char.GetHP())))
	} else if mp > 0 {
		char.SetMP(char.GetMP() + mp)

		if char.GetMP() > char.GetMaxMP() {
			char.SetMP(char.GetMaxMP())
		}

		conn.Write(statChangePacket(true, mpID, uint32(char.GetMP())))
	}

	// If in party return id and new hp, then update hp bar for party members
}

func HandleUpdateSkillRecord(conn interfaces.ClientConn, reader gopacket.Reader) {
	char := charsPtr.GetOnlineCharacterHandle(conn)

	skillID := reader.ReadUint32()

	newSP := char.GetSP() - 1
	char.SetSP(newSP)

	skills := char.GetSkills()

	newLevel := uint32(0)

	if _, exists := skills[skillID]; exists {
		newLevel = skills[skillID] + 1
	} else {
		newLevel = 1
	}

	skills[skillID] = newLevel

	conn.Write(statChangePacket(true, spID, uint32(newSP)))
	conn.Write(skillBookUpdatePacket(skillID, newLevel))
}

func HandleChangeStat(conn interfaces.ClientConn, reader gopacket.Reader) {
	char := charsPtr.GetOnlineCharacterHandle(conn)

	if char.GetAP() == 0 {
		return
	}

	stat := reader.ReadUint32()
	var value uint16

	maxDice := uint16(2000)
	maxHpMp := uint16(30000)

	switch stat {
	case strID:
		if char.GetStr() >= maxDice {
			conn.Write(statNoChangePacket())
			return
		}

		value = char.GetStr() + 1
		char.SetStr(value)
	case dexID:
		if char.GetDex() >= maxDice {
			conn.Write(statNoChangePacket())
			return
		}

		value = char.GetDex() + 1
		char.SetDex(value)
	case intID:
		if char.GetInt() >= maxDice {
			conn.Write(statNoChangePacket())
			return
		}

		value = char.GetInt() + 1
		char.SetInt(value)
	case lukID:
		if char.GetLuk() >= maxDice {
			conn.Write(statNoChangePacket())
			return
		}

		value = char.GetLuk() + 1
		char.SetLuk(value)
	case maxHpID:
		if char.GetMaxHP() >= maxHpMp {
			conn.Write(statNoChangePacket())
			return
		}

		value = char.GetMaxHP() + 1
		char.SetMaxHP(value)
	case maxMpID:
		if char.GetMaxHP() >= maxHpMp {
			conn.Write(statNoChangePacket())
			return
		}

		value = char.GetMaxMP() + 1
		char.SetMaxMP(value)
	default:
		log.Println("Unknown stat ID:", stat)
	}

	newAP := char.GetAP() - 1
	conn.Write(statChangePacket(true, stat, uint32(value)))
	conn.Write(statChangePacket(true, apID, uint32(newAP)))
	char.SetAP(newAP)
}