package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"strings"
	"time"

	kq "github.com/ughoavgfhw/libkq"
	. "github.com/ughoavgfhw/libkq/common"
	"github.com/ughoavgfhw/libkq/io"
	"github.com/ughoavgfhw/libkq/maps"
	"github.com/ughoavgfhw/libkq/parser"
)

var (
	msgDump       = ioutil.Discard
	snailDebug    = ioutil.Discard
	logOut        = os.Stderr
	predictionOut = os.Stdout
	csvOut        io.Writer // Opened in main
	replayLog     io.Writer // Opened in main
)

type gateData struct {
	index int
	typ   GateType
}

var gateMap map[Position]gateData

const teamSidesSwapped = false
const mapCenter = 960

var snailTime time.Time
var snailSpeed = 0.0

func initForMap(m Map, game *kq.GameState) {
	game.Map = m
	meta := maps.MetadataForMap(m)

	gateMap = make(map[Position]gateData)
	game.WarriorGates = make([]kq.GateState, len(meta.WarriorGates))
	for i, g := range meta.WarriorGates {
		gateMap[g.Pos] = gateData{i, WarriorGate}
	}
	game.SpeedGates = make([]kq.GateState, len(meta.SpeedGates))
	for i, g := range meta.SpeedGates {
		gateMap[g.Pos] = gateData{i, SpeedGate}
	}

	game.Snails = make([]kq.SnailState, len(meta.Snails))
	for i, s := range meta.Snails {
		game.Snails[i].MaxPos = (s.Nets[1].X + s.Nets[0].X) / 2
	}
}
func snailEstimate(t time.Time, game *kq.GameState) int {
	if !t.After(snailTime) {
		return game.Snails[0].Pos
	}
	return game.Snails[0].Pos + int((snailSpeed*float64(t.Sub(snailTime)))/float64(time.Second))
}
func checkForFamine(when time.Time, game *kq.GameState) {
	maxBerries := maps.MetadataForMap(game.Map).BerriesAvailable
	if game.BerriesUsed == maxBerries {
		game.StartFamine(when)
		fmt.Fprintln(logOut, "Start famine: ", when)
	}
}
func updateState(msg kqio.Message, game *kq.GameState) bool {
	if game.InGame() && game.InFamine() && msg.Time.Sub(game.FamineStart) > famineDuration {
		game.EndFamine()
		fmt.Fprintln(logOut, "End famine: ", msg.Time)
	}
	switch msg.Type {
	case "alive":
		return true // Trigger periodic outputs even if nothing is happening
	case "playernames", "glance", "reserveMaiden", "unreserveMaiden":
		return false
	case "gamestart":
		// Reset the state, but keep player types since spawn messages come before gamestart.
		m := msg.Val.(parser.GameStartMessage).Map
		meta := maps.MetadataForMap(m)
		var playerTypes [NumPlayers]PlayerType
		for i := 0; i < NumPlayers; i++ {
			playerTypes[i] = game.Players[i].Type
		}
		*game = kq.GameState{}
		for i := 0; i < NumPlayers; i++ {
			switch {
			case meta.FirstLifeSpeedQueen && playerTypes[i] == Queen:
				game.Players[i].Type = Queen
				game.Players[i].HasSpeed = true
			case meta.FirstLifeSpeedWarrior && playerTypes[i] == Drone:
				game.Players[i].Type = Warrior
				game.Players[i].HasSpeed = true
				switch PlayerId(i + 1).Team() {
				case BlueSide:
					game.BlueTeam.Warriors++
					game.BlueTeam.SpeedWarriors++
				case GoldSide:
					game.GoldTeam.Warriors++
					game.GoldTeam.SpeedWarriors++
				}
			default:
				game.Players[i].Type = playerTypes[i]
			}
		}
		game.Start = msg.Time
		snailSpeed = 0
		snailTime = msg.Time
		initForMap(m, game)
	case "gameend":
		game.End = msg.Time
	case "spawn":
		data := msg.Val.(parser.PlayerSpawnMessage)
		p := &game.Players[data.Player.Index()]
		p.Respawn() // Drop berry, get off snail, etc.
		p.Type = data.Type
		if game.InGame() && data.Type == Drone && maps.MetadataForMap(game.Map).FirstLifeSpeedWarrior {
			p.Type = Warrior
			p.HasSpeed = true
			switch data.Player.Team() {
			case BlueSide:
				game.BlueTeam.Warriors++
				game.BlueTeam.SpeedWarriors++
			case GoldSide:
				game.GoldTeam.Warriors++
				game.GoldTeam.SpeedWarriors++
			}
		}
	case "carryFood":
		if !game.InGame() {
			return false
		}
		data := msg.Val.(parser.PickUpBerryMessage)
		game.Players[data.Player.Index()].HasBerry = true
	case "useMaiden":
		if !game.InGame() {
			return false
		}
		data := msg.Val.(parser.UseGateMessage)
		game.Players[data.Player.Index()].HasBerry = false
		game.BerriesUsed++
		checkForFamine(msg.Time, game)
		p := &game.Players[data.Player.Index()]
		switch data.Type {
		case SpeedGate:
			p.HasSpeed = true
		case WarriorGate:
			p.Type = Warrior
			switch data.Player.Team() {
			case BlueSide:
				game.BlueTeam.Warriors++
				if p.HasSpeed {
					game.BlueTeam.SpeedWarriors++
				}
			case GoldSide:
				game.GoldTeam.Warriors++
				if p.HasSpeed {
					game.GoldTeam.SpeedWarriors++
				}
			}
		}
	case "blessMaiden":
		// TODO: Might be able to tag speed gates before gamestart on day.
		if !game.InGame() {
			return false
		}
		data := msg.Val.(parser.ClaimGateMessage)
		i := gateMap[data.Pos].index
		switch gateMap[data.Pos].typ {
		case SpeedGate:
			game.SpeedGates[i].ClaimedBy = data.Side
		case WarriorGate:
			game.WarriorGates[i].ClaimedBy = data.Side
		}
	case "playerKill":
		// TODO: Maybe can have kills before gamestart on trap map due to missing barriers
		if !game.InGame() {
			return false
		}
		data := msg.Val.(parser.PlayerKillMessage)
		v := &game.Players[data.Victim.Index()]
		switch v.Type {
		case Warrior:
			switch data.Victim.Team() {
			case BlueSide:
				game.BlueTeam.Warriors--
				if v.HasSpeed {
					game.BlueTeam.SpeedWarriors--
				}
			case GoldSide:
				game.GoldTeam.Warriors--
				if v.HasSpeed {
					game.GoldTeam.SpeedWarriors--
				}
			}
		case Queen:
			switch data.Victim.Team() {
			case BlueSide:
				game.BlueTeam.QueenDeaths++
			case GoldSide:
				game.GoldTeam.QueenDeaths++
			}
		case Drone, Robot:
			break
		}
		v.Respawn()
		if game.Players[data.Killer.Index()].OnSnail == 1 {
			snailTime = msg.Time // Position set at start of eating.
		}
	case "getOnSnail: ":
		if !game.InGame() {
			return false
		}
		data := msg.Val.(parser.GetOnSnailMessage)
		game.Players[data.Rider.Index()].OnSnail = 1
		pos := data.Pos.X - game.Snails[0].MaxPos
		if pos != game.Snails[0].Pos {
			fmt.Fprintf(snailDebug, "%v: The snail moved from %v to %v without a rider\n",
				msg.Time, game.Snails[0].Pos, pos)
		}
		// running drone speed 250 px/s. may be 1925ish pixels to wrap
		// robot 200 px/s
		// eat takes 3.5s, arantius vid says 3.67
		if game.Players[data.Rider.Index()].HasSpeed {
			fmt.Fprintln(snailDebug, "rider has speed")
			snailSpeed = 28.209890875 // 27
		} else {
			fmt.Fprintln(snailDebug, "rider is slow")
			snailSpeed = 20.896215463 // 20
		}
		if data.Rider.Team() == BlueSide {
			snailSpeed = -snailSpeed
		}
		game.Snails[0].Pos = pos
		snailTime = msg.Time
	case "getOffSnail: ":
		if !game.InGame() {
			return false
		}
		data := msg.Val.(parser.GetOffSnailMessage)
		game.Players[data.Rider.Index()].OnSnail = 0
		fmt.Fprintf(snailDebug, "Off: The snail moved by %v pixels in %v, %v px/s\n",
			data.Pos.X-game.Snails[0].MaxPos-game.Snails[0].Pos,
			msg.Time.Sub(snailTime),
			float64(data.Pos.X-game.Snails[0].MaxPos-game.Snails[0].Pos)/
				float64(msg.Time.Sub(snailTime)/time.Millisecond)*1000)
		fmt.Fprintf(snailDebug, "Estimated snail position is %v, actual is %v, diff %v\n",
			snailEstimate(msg.Time, game), data.Pos.X-game.Snails[0].MaxPos, data.Pos.X-game.Snails[0].MaxPos-snailEstimate(msg.Time, game))
		game.Snails[0].Pos = data.Pos.X - game.Snails[0].MaxPos
		snailTime = msg.Time
		snailSpeed = 0
	case "snailEat":
		if !game.InGame() {
			return false
		}
		data := msg.Val.(parser.SnailStartEatMessage)
		fmt.Fprintf(snailDebug, "Eat: The snail moved by %v pixels in %v, %v px/s\n",
			data.Pos.X-game.Snails[0].MaxPos-game.Snails[0].Pos,
			msg.Time.Sub(snailTime),
			float64(data.Pos.X-game.Snails[0].MaxPos-game.Snails[0].Pos)/
				float64(msg.Time.Sub(snailTime)/time.Millisecond)*1000)
		fmt.Fprintf(snailDebug, "Estimated snail position is %v, actual is %v, diff %v\n",
			snailEstimate(msg.Time, game), data.Pos.X-game.Snails[0].MaxPos, data.Pos.X-game.Snails[0].MaxPos-snailEstimate(msg.Time, game))
		game.Snails[0].Pos = data.Pos.X - game.Snails[0].MaxPos
		snailTime = msg.Time.Add(3500 * time.Millisecond)
	case "snailEscape":
		if !game.InGame() {
			return false
		}
		data := msg.Val.(parser.SnailEscapeEatMessage)
		// The escape event occurs at the snail's mouth, 50 pixels from it's position.
		var offset int
		if data.Escapee.Team() == BlueSide {
			offset = -50
		} else {
			offset = 50
		}
		pos := data.Pos.X - game.Snails[0].MaxPos + offset
		if pos != game.Snails[0].Pos {
			// In theory, the snail shouldn't move while someone is sacrificing.
			// In practice it can, either because it got pushed with a berry or
			// because the sacrifice carried momentum into the snail.
			fmt.Fprintf(snailDebug, "%v: The snail moved from %v to %v during a sacrifice\n",
				msg.Time, game.Snails[0].Pos, pos)
		}
		game.Snails[0].Pos = pos
		snailTime = msg.Time
	case "berryDeposit":
		if !game.InGame() {
			return false
		}
		data := msg.Val.(parser.DepositBerryMessage)
		game.Players[data.Player.Index()].HasBerry = false
		switch data.Player.Team() {
		case BlueSide:
			game.BlueTeam.BerriesIn++
		case GoldSide:
			game.GoldTeam.BerriesIn++
		}
		game.BerriesUsed++
		checkForFamine(msg.Time, game)
	case "berryKickIn":
		if !game.InGame() {
			return false
		}
		data := msg.Val.(parser.KickInBerryMessage)
		if (data.Pos.X < mapCenter) != teamSidesSwapped {
			game.BlueTeam.BerriesIn++
		} else {
			game.GoldTeam.BerriesIn++
		}
		game.BerriesUsed++
		checkForFamine(msg.Time, game)
	case "victory":
		data := msg.Val.(parser.GameResultMessage)
		game.Winner = data.Winner
		game.EndCondition = data.EndCondition
		fmt.Fprintln(logOut, game.Winner, "wins on", game.Map, "by", game.EndCondition)
	default:
		fmt.Fprintln(logOut, "Unhandled", msg)
		return false
	}
	return true
}

type CsvBuilder struct {
	strings.Builder
}

func (this *CsvBuilder) Append(items ...interface{}) {
	for _, it := range items {
		if this.Len() != 0 {
			this.WriteRune(',')
		}
		fmt.Fprint(this, it)
	}
}

const CsvHeader = "map,time_millis,gold_queen_type,gold_queen_speed,gold_queen_berry,gold_queen_snail,blue_queen_type,blue_queen_speed,blue_queen_berry,blue_queen_snail,gold_stripes_type,gold_stripes_speed,gold_stripes_berry,gold_stripes_snail,blue_stripes_type,blue_stripes_speed,blue_stripes_berry,blue_stripes_snail,gold_abs_type,gold_abs_speed,gold_abs_berry,gold_abs_snail,blue_abs_type,blue_abs_speed,blue_abs_berry,blue_abs_snail,gold_skulls_type,gold_skulls_speed,gold_skulls_berry,gold_skulls_snail,blue_skulls_type,blue_skulls_speed,blue_skulls_berry,blue_skulls_snail,gold_checks_type,gold_checks_speed,gold_checks_berry,gold_checks_snail,blue_checks_type,blue_checks_speed,blue_checks_berry,blue_checks_snail,gold_warriors,gold_queen_deaths,gold_berries,blue_warriors,blue_queen_deaths,blue_berries,snail_pos_last,snail_pos_estimate,snail_owner,snail_has_speed,warrior_gate_owner0,warrior_gate_owner1,warrior_gate_owner2,speed_gate_owner0,speed_gate_owner1,winner,end_condition"

type CsvPrinter struct {
	Map      Map
	Duration time.Duration
	Time     time.Time
	State    kq.GameState
}

func (this *CsvPrinter) String() string {
	var b CsvBuilder
	b.Append(this.Map, int64(this.Duration/time.Millisecond))
	var rider PlayerId
	for i, p := range this.State.Players {
		b.Append(p.Type, p.HasSpeed, p.HasBerry, p.IsOnSnail())
		if p.IsOnSnail() {
			rider = PlayerId(i + 1)
		}
	}
	b.Append(this.State.GoldTeam.Warriors, this.State.GoldTeam.QueenDeaths, this.State.GoldTeam.BerriesIn)
	b.Append(this.State.BlueTeam.Warriors, this.State.BlueTeam.QueenDeaths, this.State.BlueTeam.BerriesIn)
	b.Append(this.State.Snails[0].Pos, snailEstimate(this.Time, &this.State))
	if rider != 0 {
		b.Append(rider.Team(), this.State.Players[rider.Index()].HasSpeed)
	} else {
		b.Append(Neutral, false)
	}
	b.Append(this.State.WarriorGates[0].ClaimedBy, this.State.WarriorGates[1].ClaimedBy, this.State.WarriorGates[2].ClaimedBy)
	b.Append(this.State.SpeedGates[0].ClaimedBy, this.State.SpeedGates[1].ClaimedBy)
	b.Append(this.State.Winner, this.State.EndCondition)
	return b.String()
}

// snailEscape position is 50px in front of actual snail (in rider's direction)
// playerKill of rider is at snail position; note a getOffSnail comes just before the kill
// playerKill of sacrifice may be where they were just before the eat triggered
// - if coming from behind on ground, 15-16px behind snail position
// - if coming from front on ground, 66-69px in front of snail position
// - drone on ground is 9px above snail position
// - it seems you can sac from further away if in the air, at least in front
// i have seen a player killed while being eaten (playerKill 1ms after snailEat). they later escaped the eat and got on the snail 1.5 seconds after
// day/dusk snail is at y position 11 (drone at 20). night at 491 (drone 500)

const (
	berryWeight        = 100. / 12.
	snailWeight        = 200.
	lifeWeight         = 100. / 3.
	warriorWeight      = 100. / 4.
	speedWarriorWeight = warriorWeight * 4. / 3. // Matches a queen right now.

	berryBonus1At, berryBonus1Rate, berryBonus1 = 4, 1., 37.5  // 25
	berryBonus2At, berryBonus2Rate, berryBonus2 = 8, 1., 75    // 50
	berryBonus3At, berryBonus3Rate, berryBonus3 = 10, 2.5, 100 // 1.5,  100
	berryBonus4At, berryBonus4Rate, berryBonus4 = 11, 3., 250
	snailBonusMinEligible                       = 0.55
	snailBonus1At, snailBonus1Rate, snailBonus1 = 0.7, 25, 25
	snailBonus2At, snailBonus2Rate, snailBonus2 = 0.8, 15, 50   // 25,  50
	snailBonus3At, snailBonus3Rate, snailBonus3 = 0.9, 25, 100  // 50,  100
	snailBonus4At, snailBonus4Rate, snailBonus4 = 0.95, 75, 200 // 100, 200
	warriorBonus1At, warriorBonus1              = 2, 100
	queenBonus1At, queenBonus1                  = 2, 100

	objectiveBonusFactorAtFullMil = 0.5
	faminePeakWarriorWeightFactor = 2.5
	famineDuration                = 90 * time.Second

	winPointsWeight, losePointsWeight = 1, 1
)

func logistic(value, half_amplitude, rate, center float64) float64 {
	return half_amplitude * 2 / (1 + math.Exp(rate*(center-value)))
}

func modelSumLose(game *kq.GameState, when time.Time) float64 {
	snailEst := snailEstimate(when, game)
	meta := maps.MetadataForMap(game.Map)
	snailLim := (meta.Snails[0].Nets[1].X - meta.Snails[0].Nets[0].X) / 2
	maxBerries := meta.BerriesAvailable
	queenStartLives := meta.QueenLives

	snailPos := (float64(snailEst)/float64(snailLim) + 1) / 2
	if snailPos < -1 {
		snailPos = -1
	} else if snailPos > 1 {
		snailPos = 1
	}
	var blueSnail, goldSnail float64
	if teamSidesSwapped {
		goldSnail = 1 - snailPos
		blueSnail = snailPos
	} else {
		blueSnail = 1 - snailPos
		goldSnail = snailPos
	}

	var famineBerryBonusFactor, famineWarriorWeightFactor float64
	if game.InFamine() {
		progress := float64(when.Sub(game.FamineStart)) / float64(famineDuration)
		famineBerryBonusFactor = 0
		famineWarriorWeightFactor = faminePeakWarriorWeightFactor
		if progress > 0.6 {
			famineBerryBonusFactor = logistic(progress, 0.5, 30, 0.85)
			famineWarriorWeightFactor =
				1 + logistic(progress, (faminePeakWarriorWeightFactor-1.)/2., -30, 0.85)
		}
	} else {
		progress := float64(game.BerriesUsed) / float64(maxBerries)
		famineBerryBonusFactor = 1
		famineWarriorWeightFactor = 1
		if progress > 0.6 {
			famineBerryBonusFactor = logistic(progress, 0.5, -30, 0.85)
			famineWarriorWeightFactor =
				1 + logistic(progress, (faminePeakWarriorWeightFactor-1.)/2., 30, 0.85)
		}
	}

	var blueWin, blueLose, goldWin, goldLose float64
	blueLose += 100 - float64(game.GoldTeam.BerriesIn)*berryWeight
	goldLose += 100 - float64(game.BlueTeam.BerriesIn)*berryWeight
	blueLose += 100 - float64(game.BlueTeam.QueenDeaths)*lifeWeight
	goldLose += 100 - float64(game.GoldTeam.QueenDeaths)*lifeWeight
	blueWin += (float64(game.BlueTeam.Warriors-game.BlueTeam.SpeedWarriors)*warriorWeight +
		float64(game.BlueTeam.SpeedWarriors)*speedWarriorWeight) *
		famineWarriorWeightFactor
	goldWin += (float64(game.GoldTeam.Warriors-game.GoldTeam.SpeedWarriors)*warriorWeight +
		float64(game.GoldTeam.SpeedWarriors)*speedWarriorWeight) *
		famineWarriorWeightFactor
	if goldSnail <= 0.5 {
		blueLose += snailWeight / 2
	} else {
		blueLose += (1 - goldSnail) * snailWeight
	}
	if blueSnail <= 0.5 {
		goldLose += snailWeight / 2
	} else {
		goldLose += (1 - blueSnail) * snailWeight
	}

	blueFullMilFactor, goldFullMilFactor := 1., 1.
	if game.BlueTeam.Warriors == 4 {
		blueFullMilFactor = objectiveBonusFactorAtFullMil
	}
	if game.GoldTeam.Warriors == 4 {
		goldFullMilFactor = objectiveBonusFactorAtFullMil
	}

	if game.BlueTeam.BerriesIn > 0 {
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus1, berryBonus1Rate, berryBonus1At) * blueFullMilFactor * famineBerryBonusFactor
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus2, berryBonus2Rate, berryBonus2At) * blueFullMilFactor * famineBerryBonusFactor
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus3, berryBonus3Rate, berryBonus3At) * blueFullMilFactor * famineBerryBonusFactor
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus4, berryBonus4Rate, berryBonus4At) * blueFullMilFactor * famineBerryBonusFactor
	}
	if game.GoldTeam.BerriesIn > 0 {
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus1, berryBonus1Rate, berryBonus1At) * goldFullMilFactor * famineBerryBonusFactor
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus2, berryBonus2Rate, berryBonus2At) * goldFullMilFactor * famineBerryBonusFactor
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus3, berryBonus3Rate, berryBonus3At) * goldFullMilFactor * famineBerryBonusFactor
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus4, berryBonus4Rate, berryBonus4At) * goldFullMilFactor * famineBerryBonusFactor
	}
	if blueSnail >= snailBonusMinEligible {
		blueWin += logistic(blueSnail, snailBonus1, snailBonus1Rate, snailBonus1At) * blueFullMilFactor
		blueWin += logistic(blueSnail, snailBonus2, snailBonus2Rate, snailBonus2At) * blueFullMilFactor
		blueWin += logistic(blueSnail, snailBonus3, snailBonus3Rate, snailBonus3At) * blueFullMilFactor
		blueWin += logistic(blueSnail, snailBonus4, snailBonus4Rate, snailBonus4At) * blueFullMilFactor
	}
	if goldSnail >= snailBonusMinEligible {
		goldWin += logistic(goldSnail, snailBonus1, snailBonus1Rate, snailBonus1At) * goldFullMilFactor
		goldWin += logistic(goldSnail, snailBonus2, snailBonus2Rate, snailBonus2At) * goldFullMilFactor
		goldWin += logistic(goldSnail, snailBonus3, snailBonus3Rate, snailBonus3At) * goldFullMilFactor
		goldWin += logistic(goldSnail, snailBonus4, snailBonus4Rate, snailBonus4At) * goldFullMilFactor
	}
	if game.BlueTeam.Warriors-game.GoldTeam.Warriors >= warriorBonus1At {
		blueWin += warriorBonus1
	}
	if game.GoldTeam.Warriors-game.BlueTeam.Warriors >= warriorBonus1At {
		goldWin += warriorBonus1
	}
	if queenStartLives-game.BlueTeam.QueenDeaths >= queenBonus1At {
		blueWin += queenBonus1
	}
	if queenStartLives-game.GoldTeam.QueenDeaths >= queenBonus1At {
		goldWin += queenBonus1
	}

	blue := blueWin*winPointsWeight + blueLose*losePointsWeight
	gold := goldWin*winPointsWeight + goldLose*losePointsWeight

	total := blue + gold
	if teamSidesSwapped {
		return blue / total
	} else {
		return gold / total
	}
}

func modelMultLose(game *kq.GameState, when time.Time) float64 {
	snailEst := snailEstimate(when, game)
	meta := maps.MetadataForMap(game.Map)
	snailLim := (meta.Snails[0].Nets[1].X - meta.Snails[0].Nets[0].X) / 2
	maxBerries := meta.BerriesAvailable
	queenStartLives := meta.QueenLives

	snailPos := (float64(snailEst)/float64(snailLim) + 1) / 2
	if snailPos < -1 {
		snailPos = -1
	} else if snailPos > 1 {
		snailPos = 1
	}
	var blueSnail, goldSnail float64
	if teamSidesSwapped {
		goldSnail = 1 - snailPos
		blueSnail = snailPos
	} else {
		blueSnail = 1 - snailPos
		goldSnail = snailPos
	}

	var famineBerryBonusFactor, famineWarriorWeightFactor float64
	if game.InFamine() {
		progress := float64(when.Sub(game.FamineStart)) / float64(famineDuration)
		famineBerryBonusFactor = 0
		famineWarriorWeightFactor = faminePeakWarriorWeightFactor
		if progress > 0.6 {
			famineBerryBonusFactor = logistic(progress, 0.5, 30, 0.85)
			famineWarriorWeightFactor =
				1 + logistic(progress, (faminePeakWarriorWeightFactor-1.)/2., -30, 0.85)
		}
	} else {
		progress := float64(game.BerriesUsed) / float64(maxBerries)
		famineBerryBonusFactor = 1
		famineWarriorWeightFactor = 1
		if progress > 0.6 {
			famineBerryBonusFactor = logistic(progress, 0.5, -30, 0.85)
			famineWarriorWeightFactor =
				1 + logistic(progress, (faminePeakWarriorWeightFactor-1.)/2., 30, 0.85)
		}
	}

	blueWin, blueLose, goldWin, goldLose := 0., 1., 0., 1.
	blueLose *= 1 - float64(game.GoldTeam.BerriesIn)*(berryWeight/100)
	goldLose *= 1 - float64(game.BlueTeam.BerriesIn)*(berryWeight/100)
	blueLose *= 1 - float64(game.BlueTeam.QueenDeaths)*(lifeWeight/100)
	goldLose *= 1 - float64(game.GoldTeam.QueenDeaths)*(lifeWeight/100)
	blueWin += (float64(game.BlueTeam.Warriors-game.BlueTeam.SpeedWarriors)*warriorWeight +
		float64(game.BlueTeam.SpeedWarriors)*speedWarriorWeight) *
		famineWarriorWeightFactor
	goldWin += (float64(game.GoldTeam.Warriors-game.GoldTeam.SpeedWarriors)*warriorWeight +
		float64(game.GoldTeam.SpeedWarriors)*speedWarriorWeight) *
		famineWarriorWeightFactor
	if goldSnail <= 0.5 {
		blueLose *= 1
	} else {
		blueLose *= (1 - goldSnail) * (snailWeight / 100)
	}
	if blueSnail <= 0.5 {
		goldLose *= 1
	} else {
		goldLose *= (1 - blueSnail) * (snailWeight / 100)
	}

	blueFullMilFactor, goldFullMilFactor := 1., 1.
	if game.BlueTeam.Warriors == 4 {
		blueFullMilFactor = objectiveBonusFactorAtFullMil
	}
	if game.GoldTeam.Warriors == 4 {
		goldFullMilFactor = objectiveBonusFactorAtFullMil
	}

	if game.BlueTeam.BerriesIn > 0 {
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus1, berryBonus1Rate, berryBonus1At) * blueFullMilFactor * famineBerryBonusFactor
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus2, berryBonus2Rate, berryBonus2At) * blueFullMilFactor * famineBerryBonusFactor
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus3, berryBonus3Rate, berryBonus3At) * blueFullMilFactor * famineBerryBonusFactor
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus4, berryBonus4Rate, berryBonus4At) * blueFullMilFactor * famineBerryBonusFactor
	}
	if game.GoldTeam.BerriesIn > 0 {
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus1, berryBonus1Rate, berryBonus1At) * goldFullMilFactor * famineBerryBonusFactor
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus2, berryBonus2Rate, berryBonus2At) * goldFullMilFactor * famineBerryBonusFactor
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus3, berryBonus3Rate, berryBonus3At) * goldFullMilFactor * famineBerryBonusFactor
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus4, berryBonus4Rate, berryBonus4At) * goldFullMilFactor * famineBerryBonusFactor
	}
	if blueSnail >= snailBonusMinEligible {
		blueWin += logistic(blueSnail, snailBonus1, snailBonus1Rate, snailBonus1At) * blueFullMilFactor
		blueWin += logistic(blueSnail, snailBonus2, snailBonus2Rate, snailBonus2At) * blueFullMilFactor
		blueWin += logistic(blueSnail, snailBonus3, snailBonus3Rate, snailBonus3At) * blueFullMilFactor
		blueWin += logistic(blueSnail, snailBonus4, snailBonus4Rate, snailBonus4At) * blueFullMilFactor
	}
	if goldSnail >= snailBonusMinEligible {
		goldWin += logistic(goldSnail, snailBonus1, snailBonus1Rate, snailBonus1At) * goldFullMilFactor
		goldWin += logistic(goldSnail, snailBonus2, snailBonus2Rate, snailBonus2At) * goldFullMilFactor
		goldWin += logistic(goldSnail, snailBonus3, snailBonus3Rate, snailBonus3At) * goldFullMilFactor
		goldWin += logistic(goldSnail, snailBonus4, snailBonus4Rate, snailBonus4At) * goldFullMilFactor
	}
	if game.BlueTeam.Warriors-game.GoldTeam.Warriors >= warriorBonus1At {
		blueWin += warriorBonus1
	}
	if game.GoldTeam.Warriors-game.BlueTeam.Warriors >= warriorBonus1At {
		goldWin += warriorBonus1
	}
	if queenStartLives-game.BlueTeam.QueenDeaths >= queenBonus1At {
		blueWin += queenBonus1
	}
	if queenStartLives-game.GoldTeam.QueenDeaths >= queenBonus1At {
		goldWin += queenBonus1
	}

	const myLosePointsWeight = 500
	blue := blueWin*winPointsWeight + blueLose*myLosePointsWeight
	gold := goldWin*winPointsWeight + goldLose*myLosePointsWeight

	total := blue + gold
	if teamSidesSwapped {
		return blue / total
	} else {
		return gold / total
	}
}

func modelMultCbrt(game *kq.GameState, when time.Time) float64 {
	snailEst := snailEstimate(when, game)
	meta := maps.MetadataForMap(game.Map)
	snailLim := (meta.Snails[0].Nets[1].X - meta.Snails[0].Nets[0].X) / 2
	maxBerries := meta.BerriesAvailable
	queenStartLives := meta.QueenLives

	snailPos := (float64(snailEst)/float64(snailLim) + 1) / 2
	if snailPos < -1 {
		snailPos = -1
	} else if snailPos > 1 {
		snailPos = 1
	}
	var blueSnail, goldSnail float64
	if teamSidesSwapped {
		goldSnail = 1 - snailPos
		blueSnail = snailPos
	} else {
		blueSnail = 1 - snailPos
		goldSnail = snailPos
	}

	var famineBerryBonusFactor, famineWarriorWeightFactor float64
	if game.InFamine() {
		progress := float64(when.Sub(game.FamineStart)) / float64(famineDuration)
		famineBerryBonusFactor = 0
		famineWarriorWeightFactor = faminePeakWarriorWeightFactor
		if progress > 0.6 {
			famineBerryBonusFactor = logistic(progress, 0.5, 30, 0.85)
			famineWarriorWeightFactor =
				1 + logistic(progress, (faminePeakWarriorWeightFactor-1.)/2., -30, 0.85)
		}
	} else {
		progress := float64(game.BerriesUsed) / float64(maxBerries)
		famineBerryBonusFactor = 1
		famineWarriorWeightFactor = 1
		if progress > 0.6 {
			famineBerryBonusFactor = logistic(progress, 0.5, -30, 0.85)
			famineWarriorWeightFactor =
				1 + logistic(progress, (faminePeakWarriorWeightFactor-1.)/2., 30, 0.85)
		}
	}

	blueWin, blueLose, goldWin, goldLose := 0., 1., 0., 1.
	blueLose *= 1 - float64(game.GoldTeam.BerriesIn)*(berryWeight/100)
	goldLose *= 1 - float64(game.BlueTeam.BerriesIn)*(berryWeight/100)
	blueLose *= 1 - float64(game.BlueTeam.QueenDeaths)*(lifeWeight/100)
	goldLose *= 1 - float64(game.GoldTeam.QueenDeaths)*(lifeWeight/100)
	blueWin += (float64(game.BlueTeam.Warriors-game.BlueTeam.SpeedWarriors)*warriorWeight +
		float64(game.BlueTeam.SpeedWarriors)*speedWarriorWeight) *
		famineWarriorWeightFactor
	goldWin += (float64(game.GoldTeam.Warriors-game.GoldTeam.SpeedWarriors)*warriorWeight +
		float64(game.GoldTeam.SpeedWarriors)*speedWarriorWeight) *
		famineWarriorWeightFactor
	if goldSnail <= 0.5 {
		blueLose *= 1
	} else {
		blueLose *= (1 - goldSnail) * (snailWeight / 100)
	}
	if blueSnail <= 0.5 {
		goldLose *= 1
	} else {
		goldLose *= (1 - blueSnail) * (snailWeight / 100)
	}

	blueFullMilFactor, goldFullMilFactor := 1., 1.
	if game.BlueTeam.Warriors == 4 {
		blueFullMilFactor = objectiveBonusFactorAtFullMil
	}
	if game.GoldTeam.Warriors == 4 {
		goldFullMilFactor = objectiveBonusFactorAtFullMil
	}

	if game.BlueTeam.BerriesIn > 0 {
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus1, berryBonus1Rate, berryBonus1At) * blueFullMilFactor * famineBerryBonusFactor
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus2, berryBonus2Rate, berryBonus2At) * blueFullMilFactor * famineBerryBonusFactor
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus3, berryBonus3Rate, berryBonus3At) * blueFullMilFactor * famineBerryBonusFactor
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus4, berryBonus4Rate, berryBonus4At) * blueFullMilFactor * famineBerryBonusFactor
	}
	if game.GoldTeam.BerriesIn > 0 {
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus1, berryBonus1Rate, berryBonus1At) * goldFullMilFactor * famineBerryBonusFactor
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus2, berryBonus2Rate, berryBonus2At) * goldFullMilFactor * famineBerryBonusFactor
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus3, berryBonus3Rate, berryBonus3At) * goldFullMilFactor * famineBerryBonusFactor
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus4, berryBonus4Rate, berryBonus4At) * goldFullMilFactor * famineBerryBonusFactor
	}
	if blueSnail >= snailBonusMinEligible {
		blueWin += logistic(blueSnail, snailBonus1, snailBonus1Rate, snailBonus1At) * blueFullMilFactor
		blueWin += logistic(blueSnail, snailBonus2, snailBonus2Rate, snailBonus2At) * blueFullMilFactor
		blueWin += logistic(blueSnail, snailBonus3, snailBonus3Rate, snailBonus3At) * blueFullMilFactor
		blueWin += logistic(blueSnail, snailBonus4, snailBonus4Rate, snailBonus4At) * blueFullMilFactor
	}
	if goldSnail >= snailBonusMinEligible {
		goldWin += logistic(goldSnail, snailBonus1, snailBonus1Rate, snailBonus1At) * goldFullMilFactor
		goldWin += logistic(goldSnail, snailBonus2, snailBonus2Rate, snailBonus2At) * goldFullMilFactor
		goldWin += logistic(goldSnail, snailBonus3, snailBonus3Rate, snailBonus3At) * goldFullMilFactor
		goldWin += logistic(goldSnail, snailBonus4, snailBonus4Rate, snailBonus4At) * goldFullMilFactor
	}
	if game.BlueTeam.Warriors-game.GoldTeam.Warriors >= warriorBonus1At {
		blueWin += warriorBonus1
	}
	if game.GoldTeam.Warriors-game.BlueTeam.Warriors >= warriorBonus1At {
		goldWin += warriorBonus1
	}
	if queenStartLives-game.BlueTeam.QueenDeaths >= queenBonus1At {
		blueWin += queenBonus1
	}
	if queenStartLives-game.GoldTeam.QueenDeaths >= queenBonus1At {
		goldWin += queenBonus1
	}

	const myLosePointsWeight = 500
	blue := blueWin*winPointsWeight + math.Cbrt(blueLose)*myLosePointsWeight
	gold := goldWin*winPointsWeight + math.Cbrt(goldLose)*myLosePointsWeight

	total := blue + gold
	if teamSidesSwapped {
		return blue / total
	} else {
		return gold / total
	}
}

func modelMultSqrt(game *kq.GameState, when time.Time) float64 {
	snailEst := snailEstimate(when, game)
	meta := maps.MetadataForMap(game.Map)
	snailLim := (meta.Snails[0].Nets[1].X - meta.Snails[0].Nets[0].X) / 2
	maxBerries := meta.BerriesAvailable
	queenStartLives := meta.QueenLives

	snailPos := (float64(snailEst)/float64(snailLim) + 1) / 2
	if snailPos < -1 {
		snailPos = -1
	} else if snailPos > 1 {
		snailPos = 1
	}
	var blueSnail, goldSnail float64
	if teamSidesSwapped {
		goldSnail = 1 - snailPos
		blueSnail = snailPos
	} else {
		blueSnail = 1 - snailPos
		goldSnail = snailPos
	}

	var famineBerryBonusFactor, famineWarriorWeightFactor float64
	if game.InFamine() {
		progress := float64(when.Sub(game.FamineStart)) / float64(famineDuration)
		famineBerryBonusFactor = 0
		famineWarriorWeightFactor = faminePeakWarriorWeightFactor
		if progress > 0.6 {
			famineBerryBonusFactor = logistic(progress, 0.5, 30, 0.85)
			famineWarriorWeightFactor =
				1 + logistic(progress, (faminePeakWarriorWeightFactor-1.)/2., -30, 0.85)
		}
	} else {
		progress := float64(game.BerriesUsed) / float64(maxBerries)
		famineBerryBonusFactor = 1
		famineWarriorWeightFactor = 1
		if progress > 0.6 {
			famineBerryBonusFactor = logistic(progress, 0.5, -30, 0.85)
			famineWarriorWeightFactor =
				1 + logistic(progress, (faminePeakWarriorWeightFactor-1.)/2., 30, 0.85)
		}
	}

	blueWin, blueLose, goldWin, goldLose := 0., 1., 0., 1.
	blueLose *= 1 - float64(game.GoldTeam.BerriesIn)*(berryWeight/100)
	goldLose *= 1 - float64(game.BlueTeam.BerriesIn)*(berryWeight/100)
	blueLose *= 1 - float64(game.BlueTeam.QueenDeaths)*(lifeWeight/100)
	goldLose *= 1 - float64(game.GoldTeam.QueenDeaths)*(lifeWeight/100)
	blueWin += (float64(game.BlueTeam.Warriors-game.BlueTeam.SpeedWarriors)*warriorWeight +
		float64(game.BlueTeam.SpeedWarriors)*speedWarriorWeight) *
		famineWarriorWeightFactor
	goldWin += (float64(game.GoldTeam.Warriors-game.GoldTeam.SpeedWarriors)*warriorWeight +
		float64(game.GoldTeam.SpeedWarriors)*speedWarriorWeight) *
		famineWarriorWeightFactor
	if goldSnail <= 0.5 {
		blueLose *= 1
	} else {
		blueLose *= (1 - goldSnail) * (snailWeight / 100)
	}
	if blueSnail <= 0.5 {
		goldLose *= 1
	} else {
		goldLose *= (1 - blueSnail) * (snailWeight / 100)
	}

	blueFullMilFactor, goldFullMilFactor := 1., 1.
	if game.BlueTeam.Warriors == 4 {
		blueFullMilFactor = objectiveBonusFactorAtFullMil
	}
	if game.GoldTeam.Warriors == 4 {
		goldFullMilFactor = objectiveBonusFactorAtFullMil
	}

	if game.BlueTeam.BerriesIn > 0 {
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus1, berryBonus1Rate, berryBonus1At) * blueFullMilFactor * famineBerryBonusFactor
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus2, berryBonus2Rate, berryBonus2At) * blueFullMilFactor * famineBerryBonusFactor
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus3, berryBonus3Rate, berryBonus3At) * blueFullMilFactor * famineBerryBonusFactor
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus4, berryBonus4Rate, berryBonus4At) * blueFullMilFactor * famineBerryBonusFactor
	}
	if game.GoldTeam.BerriesIn > 0 {
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus1, berryBonus1Rate, berryBonus1At) * goldFullMilFactor * famineBerryBonusFactor
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus2, berryBonus2Rate, berryBonus2At) * goldFullMilFactor * famineBerryBonusFactor
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus3, berryBonus3Rate, berryBonus3At) * goldFullMilFactor * famineBerryBonusFactor
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus4, berryBonus4Rate, berryBonus4At) * goldFullMilFactor * famineBerryBonusFactor
	}
	if blueSnail >= snailBonusMinEligible {
		blueWin += logistic(blueSnail, snailBonus1, snailBonus1Rate, snailBonus1At) * blueFullMilFactor
		blueWin += logistic(blueSnail, snailBonus2, snailBonus2Rate, snailBonus2At) * blueFullMilFactor
		blueWin += logistic(blueSnail, snailBonus3, snailBonus3Rate, snailBonus3At) * blueFullMilFactor
		blueWin += logistic(blueSnail, snailBonus4, snailBonus4Rate, snailBonus4At) * blueFullMilFactor
	}
	if goldSnail >= snailBonusMinEligible {
		goldWin += logistic(goldSnail, snailBonus1, snailBonus1Rate, snailBonus1At) * goldFullMilFactor
		goldWin += logistic(goldSnail, snailBonus2, snailBonus2Rate, snailBonus2At) * goldFullMilFactor
		goldWin += logistic(goldSnail, snailBonus3, snailBonus3Rate, snailBonus3At) * goldFullMilFactor
		goldWin += logistic(goldSnail, snailBonus4, snailBonus4Rate, snailBonus4At) * goldFullMilFactor
	}
	if game.BlueTeam.Warriors-game.GoldTeam.Warriors >= warriorBonus1At {
		blueWin += warriorBonus1
	}
	if game.GoldTeam.Warriors-game.BlueTeam.Warriors >= warriorBonus1At {
		goldWin += warriorBonus1
	}
	if queenStartLives-game.BlueTeam.QueenDeaths >= queenBonus1At {
		blueWin += queenBonus1
	}
	if queenStartLives-game.GoldTeam.QueenDeaths >= queenBonus1At {
		goldWin += queenBonus1
	}

	const myLosePointsWeight = 500
	blue := blueWin*winPointsWeight + math.Sqrt(blueLose)*myLosePointsWeight
	gold := goldWin*winPointsWeight + math.Sqrt(goldLose)*myLosePointsWeight

	total := blue + gold
	if teamSidesSwapped {
		return blue / total
	} else {
		return gold / total
	}
}

func modelMultQueenSqrt(game *kq.GameState, when time.Time) float64 {
	snailEst := snailEstimate(when, game)
	meta := maps.MetadataForMap(game.Map)
	snailLim := (meta.Snails[0].Nets[1].X - meta.Snails[0].Nets[0].X) / 2
	maxBerries := meta.BerriesAvailable
	queenStartLives := meta.QueenLives

	snailPos := (float64(snailEst)/float64(snailLim) + 1) / 2
	if snailPos < -1 {
		snailPos = -1
	} else if snailPos > 1 {
		snailPos = 1
	}
	var blueSnail, goldSnail float64
	if teamSidesSwapped {
		goldSnail = 1 - snailPos
		blueSnail = snailPos
	} else {
		blueSnail = 1 - snailPos
		goldSnail = snailPos
	}

	var famineBerryBonusFactor, famineWarriorWeightFactor float64
	if game.InFamine() {
		progress := float64(when.Sub(game.FamineStart)) / float64(famineDuration)
		famineBerryBonusFactor = 0
		famineWarriorWeightFactor = faminePeakWarriorWeightFactor
		if progress > 0.6 {
			famineBerryBonusFactor = logistic(progress, 0.5, 30, 0.85)
			famineWarriorWeightFactor =
				1 + logistic(progress, (faminePeakWarriorWeightFactor-1.)/2., -30, 0.85)
		}
	} else {
		progress := float64(game.BerriesUsed) / float64(maxBerries)
		famineBerryBonusFactor = 1
		famineWarriorWeightFactor = 1
		if progress > 0.6 {
			famineBerryBonusFactor = logistic(progress, 0.5, -30, 0.85)
			famineWarriorWeightFactor =
				1 + logistic(progress, (faminePeakWarriorWeightFactor-1.)/2., 30, 0.85)
		}
	}

	blueWin, blueLose, goldWin, goldLose := 0., 1., 0., 1.
	blueLose *= 1 - float64(game.GoldTeam.BerriesIn)*(berryWeight/100)
	goldLose *= 1 - float64(game.BlueTeam.BerriesIn)*(berryWeight/100)
	blueLose *= math.Sqrt(1 - float64(game.BlueTeam.QueenDeaths)*(lifeWeight/100))
	goldLose *= math.Sqrt(1 - float64(game.GoldTeam.QueenDeaths)*(lifeWeight/100))
	blueWin += (float64(game.BlueTeam.Warriors-game.BlueTeam.SpeedWarriors)*warriorWeight +
		float64(game.BlueTeam.SpeedWarriors)*speedWarriorWeight) *
		famineWarriorWeightFactor
	goldWin += (float64(game.GoldTeam.Warriors-game.GoldTeam.SpeedWarriors)*warriorWeight +
		float64(game.GoldTeam.SpeedWarriors)*speedWarriorWeight) *
		famineWarriorWeightFactor
	if goldSnail <= 0.5 {
		blueLose *= 1
	} else {
		blueLose *= (1 - goldSnail) * (snailWeight / 100)
	}
	if blueSnail <= 0.5 {
		goldLose *= 1
	} else {
		goldLose *= (1 - blueSnail) * (snailWeight / 100)
	}

	blueFullMilFactor, goldFullMilFactor := 1., 1.
	if game.BlueTeam.Warriors == 4 {
		blueFullMilFactor = objectiveBonusFactorAtFullMil
	}
	if game.GoldTeam.Warriors == 4 {
		goldFullMilFactor = objectiveBonusFactorAtFullMil
	}

	if game.BlueTeam.BerriesIn > 0 {
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus1, berryBonus1Rate, berryBonus1At) * blueFullMilFactor * famineBerryBonusFactor
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus2, berryBonus2Rate, berryBonus2At) * blueFullMilFactor * famineBerryBonusFactor
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus3, berryBonus3Rate, berryBonus3At) * blueFullMilFactor * famineBerryBonusFactor
		blueWin += logistic(float64(game.BlueTeam.BerriesIn), berryBonus4, berryBonus4Rate, berryBonus4At) * blueFullMilFactor * famineBerryBonusFactor
	}
	if game.GoldTeam.BerriesIn > 0 {
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus1, berryBonus1Rate, berryBonus1At) * goldFullMilFactor * famineBerryBonusFactor
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus2, berryBonus2Rate, berryBonus2At) * goldFullMilFactor * famineBerryBonusFactor
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus3, berryBonus3Rate, berryBonus3At) * goldFullMilFactor * famineBerryBonusFactor
		goldWin += logistic(float64(game.GoldTeam.BerriesIn), berryBonus4, berryBonus4Rate, berryBonus4At) * goldFullMilFactor * famineBerryBonusFactor
	}
	if blueSnail >= snailBonusMinEligible {
		blueWin += logistic(blueSnail, snailBonus1, snailBonus1Rate, snailBonus1At) * blueFullMilFactor
		blueWin += logistic(blueSnail, snailBonus2, snailBonus2Rate, snailBonus2At) * blueFullMilFactor
		blueWin += logistic(blueSnail, snailBonus3, snailBonus3Rate, snailBonus3At) * blueFullMilFactor
		blueWin += logistic(blueSnail, snailBonus4, snailBonus4Rate, snailBonus4At) * blueFullMilFactor
	}
	if goldSnail >= snailBonusMinEligible {
		goldWin += logistic(goldSnail, snailBonus1, snailBonus1Rate, snailBonus1At) * goldFullMilFactor
		goldWin += logistic(goldSnail, snailBonus2, snailBonus2Rate, snailBonus2At) * goldFullMilFactor
		goldWin += logistic(goldSnail, snailBonus3, snailBonus3Rate, snailBonus3At) * goldFullMilFactor
		goldWin += logistic(goldSnail, snailBonus4, snailBonus4Rate, snailBonus4At) * goldFullMilFactor
	}
	if game.BlueTeam.Warriors-game.GoldTeam.Warriors >= warriorBonus1At {
		blueWin += warriorBonus1
	}
	if game.GoldTeam.Warriors-game.BlueTeam.Warriors >= warriorBonus1At {
		goldWin += warriorBonus1
	}
	if queenStartLives-game.BlueTeam.QueenDeaths >= queenBonus1At {
		blueWin += queenBonus1
	}
	if queenStartLives-game.GoldTeam.QueenDeaths >= queenBonus1At {
		goldWin += queenBonus1
	}

	const myLosePointsWeight = 500
	blue := blueWin*winPointsWeight + blueLose*myLosePointsWeight
	gold := goldWin*winPointsWeight + goldLose*myLosePointsWeight

	total := blue + gold
	if teamSidesSwapped {
		return blue / total
	} else {
		return gold / total
	}
}

type autoConnector struct {
	conn    *kqio.CabConnection
	connect func() (*kqio.CabConnection, error)
}

func (ac *autoConnector) EnsureConnected() {
	var err error
	delay := time.Second
	for ac.conn == nil {
		if ac.conn, err = ac.connect(); err != nil {
			fmt.Fprintf(logOut, "Failed to connect, will retry in %v: %v\n", delay, err)
			time.Sleep(delay)
		}
		delay += 500 * time.Millisecond
	}
}
func (ac *autoConnector) ReadMessageString(out *kqio.MessageString) error {
	ac.EnsureConnected()
	err := ac.conn.ReadMessageString(out)
	if err != nil && err != io.EOF {
		fmt.Fprintf(logOut, "Read error, closing connection: %v\n", err)
		ac.conn.Close()
		ac.conn = nil
	}
	return err
}
func (ac *autoConnector) WriteMessageString(msg *kqio.MessageString) error {
	ac.EnsureConnected()
	err := ac.conn.WriteMessageString(msg)
	if err != nil {
		fmt.Fprintf(logOut, "Write error, closing connection: %v\n", err)
		ac.conn.Close()
		ac.conn = nil
	}
	return err
}
func (ac *autoConnector) Close() error {
	var err error
	if ac.conn != nil {
		err = ac.conn.Close()
		ac.conn = nil
	}
	ac.connect = func() (*kqio.CabConnection, error) {
		panic("Attempting to auto-connect after explicit closure")
	}
	return err
}

type delayed struct {
	*autoConnector
	input chan struct{ msg *kqio.MessageString; err error }
	stop  chan struct{}
}
func (d *delayed) ReadMessageString(out *kqio.MessageString) error {
	data, ok := <-d.input
	if !ok {
		return fmt.Errorf("Attempting to read from closed delayed connection")
	}
	if data.msg != nil {
		*out = *data.msg
	}
	return data.err
}
func (d *delayed) Close() error {
	close(d.stop)
	for gotMsg := true; gotMsg; _, gotMsg = <-d.input {}
	return d.autoConnector.Close()
}

func (d *delayed) reader(delayAmount time.Duration) {
	defer close(d.input)

	type node struct {
		msg *kqio.MessageString
		err error
		offset time.Duration
		next *node
	}
	inputs := make(chan *node)

	go func() {
		defer close(inputs)
		var last time.Time
		for {
			select {
			case <-d.stop:
				return
			default:
			}

			msg := &kqio.MessageString{}
			err := d.autoConnector.ReadMessageString(msg)
			n := &node{}
			n.msg = msg
			n.err = err
			if !last.IsZero() {
				n.offset = msg.Time.Sub(last)
			}
			last = msg.Time
			inputs <- n
			if err == io.EOF {
				return
			}
		}
	}()

	var head, end *node
	if n, ok := <-inputs; ok {
		head = n
		end = n
	} else {
		return
	}
	timer := time.NewTimer(delayAmount)
	output := func() {
		n := head
		head = head.next
		if head != nil {
			timer.Reset(head.offset)
		}
		d.input <- struct{ msg *kqio.MessageString; err error }{ n.msg, n.err }
	}
	for {
		select {
		case n, ok := <-inputs:
			if !ok {
				for head != nil {
					select {
					case <-d.stop:
						head = nil
						if !timer.Stop() {
							<-timer.C
						}
					case <-timer.C:
						output()
					}
				}
				return
			}
			end.next = n
			end = n
			if head == nil {
				head = n
				timer.Reset(delayAmount)
			}
		case <-timer.C:
			output()
		}
	}
}

func delay(ac *autoConnector, amount time.Duration) *delayed {
	d := &delayed{
		ac,
		make(chan struct{ msg *kqio.MessageString; err error }),
	    make(chan struct{}),
	}
	go d.reader(amount)
	return d
}

type teeReader struct {
	kqio.MessageStringReadWriteCloser
	w kqio.MessageStringWriter
}

func (t *teeReader) ReadMessageString(out *kqio.MessageString) error {
	if e := t.MessageStringReadWriteCloser.ReadMessageString(out); e != nil {
		return e
	}
	if e := t.w.WriteMessageString(out); e != nil {
		return e
	}
	return nil
}

type playerStat struct {
	BerriesRun, BerriesKicked, BerriesKickedOpp                int
	SnailTime                                                  time.Duration
	SnailDist                                                  int
	WarriorTime, MaxWarriorTime, LastWarriorTime               time.Duration
	Kills, WarriorKills, QueenKills, DroneKills                int
	SnailKills, EatKills, InGateKills                          int
	Assists, DroneAssists, WarriorGateBumpOuts                 int
	Deaths, WarriorDeaths, DroneDeaths, SnailDeaths, EatDeaths int
	EatRescues, EatRescued                                     int

	// 100+ms after bump to get knocked from gate (bump is first). 500ms? of stun. 50ms after leaving gate for kill but sometimes 0. 50+ms after off snail for kill rarely over 50, <2 for escape
	lastBumped, lastOnSnail, lastOffSnail                time.Time
	lastLeaveWarriorGate, warriorStart, lastLockoutEvent time.Time

	lastBumper    PlayerId
	bumperType    PlayerType // In case they die between bumping and the assist.
	snailStartPos int
	inWarriorGate bool
}

var lastSnailEscape time.Time
var playerStats [NumPlayers]playerStat

func updateStats(msg *kqio.Message, state *kq.GameState) {
	if msg.Type == "gamestart" {
		playerStats = [NumPlayers]playerStat{}
	}
	if !state.InGame() && msg.Type != "victory" {
		return
	}
	switch msg.Type {
	case "glance":
		val := msg.Val.(parser.GlanceMessage)
		p1 := &playerStats[val.Player1.Index()]
		p2 := &playerStats[val.Player2.Index()]
		p1.lastBumped = msg.Time
		p1.lastBumper = val.Player2
		p1.bumperType = state.Players[val.Player2.Index()].Type
		p2.lastBumped = msg.Time
		p2.lastBumper = val.Player1
		p2.bumperType = state.Players[val.Player1.Index()].Type
		if p1.inWarriorGate {
			p2.WarriorGateBumpOuts++
		}
		if p2.inWarriorGate {
			p1.WarriorGateBumpOuts++
		}
	case "reserveMaiden":
		val := msg.Val.(parser.EnterGateMessage)
		if gateMap[val.Pos].typ == WarriorGate {
			playerStats[val.Player.Index()].inWarriorGate = true
		}
	case "unreserveMaiden":
		val := msg.Val.(parser.LeaveGateMessage)
		p := &playerStats[val.Player.Index()]
		if p.inWarriorGate {
			p.lastLeaveWarriorGate = msg.Time
		}
		p.inWarriorGate = false
	case "useMaiden":
		val := msg.Val.(parser.UseGateMessage)
		playerStats[val.Player.Index()].inWarriorGate = false
		if val.Type == WarriorGate {
			playerStats[val.Player.Index()].warriorStart = msg.Time
		}
	case "getOnSnail: ":
		val := msg.Val.(parser.GetOnSnailMessage)
		p := &playerStats[val.Rider.Index()]
		p.lastOnSnail = msg.Time
		p.snailStartPos = val.Pos.X
	case "getOffSnail: ":
		val := msg.Val.(parser.GetOffSnailMessage)
		p := &playerStats[val.Rider.Index()]
		p.SnailTime += msg.Time.Sub(p.lastOnSnail)
		if val.Rider.Team() == BlueSide {
			p.SnailDist += p.snailStartPos - val.Pos.X
		} else {
			p.SnailDist += val.Pos.X - p.snailStartPos
		}
		p.lastOffSnail = msg.Time
	case "snailEscape":
		val := msg.Val.(parser.SnailEscapeEatMessage)
		playerStats[val.Escapee.Index()].EatRescued++
		lastSnailEscape = msg.Time
	case "berryDeposit":
		val := msg.Val.(parser.DepositBerryMessage)
		playerStats[val.Player.Index()].BerriesRun++
	case "berryKickIn":
		val := msg.Val.(parser.KickInBerryMessage)
		bluePlayer := val.Player.Team() == BlueSide
		blueHive := val.Pos.X < mapCenter
		if bluePlayer == blueHive {
			playerStats[val.Player.Index()].BerriesKicked++
		} else {
			playerStats[val.Player.Index()].BerriesKickedOpp++
		}
	case "playerKill":
		val := msg.Val.(parser.PlayerKillMessage)
		k := &playerStats[val.Killer.Index()]
		v := &playerStats[val.Victim.Index()]
		k.Kills++
		v.Deaths++
		switch val.VictimType {
		case Queen:
			k.QueenKills++
		case Warrior:
			k.WarriorKills++
			v.WarriorDeaths++
			v.LastWarriorTime = msg.Time.Sub(v.warriorStart)
			v.WarriorTime += v.LastWarriorTime
			if v.LastWarriorTime > v.MaxWarriorTime {
				v.MaxWarriorTime = v.LastWarriorTime
			}
		case Drone:
			k.DroneKills++
			v.DroneDeaths++
			if state.Players[val.Killer.Index()].IsOnSnail() {
				k.EatKills++
				v.EatDeaths++
			} else if state.Players[val.Victim.Index()].IsOnSnail() ||
				(!v.lastOffSnail.IsZero() && msg.Time.Add(-60*time.Millisecond).Before(v.lastOffSnail)) {
				k.SnailKills++
				v.SnailDeaths++
				if !lastSnailEscape.IsZero() && msg.Time.Add(-60*time.Millisecond).Before(lastSnailEscape) {
					k.EatRescues++
				}
			} else if !v.lastLeaveWarriorGate.IsZero() && msg.Time.Add(-60*time.Millisecond).Before(v.lastLeaveWarriorGate) {
				k.InGateKills++
			}
		}
		if !v.lastBumped.IsZero() && msg.Time.Add(-time.Second).Before(v.lastBumped) {
			b := &playerStats[v.lastBumper.Index()]
			b.Assists++
			if v.bumperType == Drone {
				b.DroneAssists++
			}
		}
	case "victory":
		val := msg.Val.(parser.GameResultMessage)
		// Be sure to give snail rider and warriors their final credit.
		for i := 0; i < NumPlayers; i++ {
			if state.Players[i].Type != Warrior {
				continue
			}
			p := &playerStats[i]
			p.LastWarriorTime = msg.Time.Sub(p.warriorStart)
			p.WarriorTime += p.LastWarriorTime
			if p.LastWarriorTime > p.MaxWarriorTime {
				p.MaxWarriorTime = p.LastWarriorTime
			}
		}
		if val.EndCondition == SnailWin {
			var endPos int
			switch val.Winner {
			case BlueSide:
				if state.Map == NightMap {
					endPos = 270
				} else {
					endPos = 60
				}
			case GoldSide:
				if state.Map == NightMap {
					endPos = 1650
				} else {
					endPos = 1860
				}
			}
			for i := 0; i < NumPlayers; i++ {
				if state.Players[i].IsOnSnail() {
					p := &playerStats[i]
					p.SnailTime += msg.Time.Sub(p.lastOnSnail)
					if PlayerId(i+1).Team() == BlueSide {
						p.SnailDist += p.snailStartPos - endPos
					} else {
						p.SnailDist += endPos - p.snailStartPos
					}
				}
			}
		}
	}
}

var port = flag.Int("port", 8080, "the port number to listen on")
var model = flag.String("model", "sumLose", "the name of the model to use for predictions")
func main() {
	flag.Parse()
	broadcast := make(chan interface{})
	go startWebServer(fmt.Sprintf(":%d", *port), broadcast)
	<-time.After(5 * time.Second)
	webStartTime, _ := time.Parse(time.RFC3339Nano, "2018-10-20T18:39:49.376-05:00")

	var e error
	cabAddress := "ws://kq.local:12749"
	args := flag.Args()
	if len(args) >= 1 && len(args[0]) > 0 {
		cabAddress = args[0]
	}
	autoconn := delay(&autoConnector{nil, func() (*kqio.CabConnection, error) {
		fmt.Fprintln(logOut, "Attempting to connect to", cabAddress)
		return kqio.Connect(cabAddress)
	}}, 500*time.Millisecond)
	replayLog, e = os.Create(fmt.Sprint("out", time.Now().Format("2006-01-02T15-04-05-0700"), ".log"))
	if e != nil {
		panic(e)
	}
	var strReader kqio.MessageStringReader = &teeReader{autoconn, kqio.NewMessageStringWriter(replayLog)}
	defer func() { fmt.Fprintln(logOut, "Disconnecting"); autoconn.Close() }()
	score := modelSumLose
	scorers := [...]func(*kq.GameState, time.Time) float64{modelSumLose, modelMultLose, modelMultCbrt, modelMultSqrt, modelMultQueenSqrt}
	switch *model {
	case "sumLose":
		score = modelSumLose
	case "multLose":
		score = modelMultLose
	case "multCbrt":
		score = modelMultCbrt
	case "multSqrt":
		score = modelMultSqrt
	case "multQSqrt":
		score = modelMultQueenSqrt
	default:
		panic(fmt.Sprintf("Unknown model %v", *model))
	}
	reader := kq.NewCabinet(strReader)
	var msg kqio.Message
	state := &kq.GameState{}
	csvOut, e = os.Create("out.csv")
	if e != nil {
		panic(e)
	}
	fmt.Fprintln(csvOut, CsvHeader)
	rateLimitTicker := time.NewTicker(100 * time.Millisecond)
	berryCount := 0
	for {
		var rateLimitPassed bool
		select {
		case <-rateLimitTicker.C:
			rateLimitPassed = true
		default:
			rateLimitPassed = false
		}
		e = reader.ReadMessage(&msg)
		if e != nil {
			fmt.Fprintln(msgDump, "read error", e)
			if e == io.EOF {
				break
			}
			continue
		}
		fmt.Fprintln(msgDump, msg)
		updateStats(&msg, state)
		if updateState(msg, state) && !state.Start.IsZero() && (state.InGame() || msg.Type == "victory") {
			fmt.Fprintln(csvOut, &CsvPrinter{state.Map, msg.Time.Sub(state.Start), msg.Time, *state})
			s := score(state, msg.Time)
			if msg.Type == "gamestart" {
				broadcast <- msg.Time
			} else if !msg.Time.Before(webStartTime) {
				var dp dataPoint
				dp.when = msg.Time
				switch msg.Type {
				case "useMaiden", "playerKill", "getOnSnail: ", "getOffSnail: ", "snailEat", "berryDeposit", "berryKickIn", "victory":
					dp.event = fmt.Sprintf("%v %v", msg.Type, msg.Val)
				}
				for _, scorer := range scorers {
					dp.vals = append(dp.vals, scorer(state, msg.Time))
				}
				dp.stats = playerStats[:]
				for i := 0; i < NumPlayers; i++ {
					dp.status = append(dp.status, struct{ Speed, Warrior bool }{
						Speed:   state.Players[i].HasSpeed,
						Warrior: state.Players[i].Type == Warrior,
					})
				}
				dp.mp = state.Map.String()
				dp.dur = msg.Time.Sub(state.Start)
				if msg.Type == "victory" {
					dp.winner = msg.Val.(parser.GameResultMessage).Winner.String()
					dp.winType = msg.Val.(parser.GameResultMessage).EndCondition.String()
				}
				broadcast <- dp
			}
			if s <= 0.5 {
				fmt.Fprintf(predictionOut, "%*s%*v%%\n",
					int(s*80), "|",
					41-int(s*80), int((0.5-s)*200))
			} else {
				fmt.Fprintf(predictionOut, "%38v%%%*s\n",
					int((s-0.5)*200),
					int(s*80)-39, "|")
			}
		}

		if state.InGame() {
			if berries := maps.MetadataForMap(state.Map).BerriesAvailable - state.BerriesUsed; berries != berryCount || (state.InFamine() && rateLimitPassed) {
				berryCount = berries
				broadcast <- famineUpdate{berries, state.FamineStart, msg.Time}
			}
		}
	}
	rateLimitTicker.Stop()
}
