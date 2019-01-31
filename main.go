package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"strings"
	"time"

	kq "github.com/ughoavgfhw/libkq"
	"github.com/ughoavgfhw/libkq/io"
	"github.com/ughoavgfhw/libkq/maps"
	"github.com/ughoavgfhw/libkq/parser"
	. "github.com/ughoavgfhw/libkq/common"
)

var (
	msgDump = ioutil.Discard
	snailDebug = ioutil.Discard
	logOut = os.Stderr
	predictionOut = os.Stdout
	csvOut io.Writer  // Opened in main
	replayLog io.Writer  // Opened in main
)

type gateData struct{
	index int
	typ GateType
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
	if !t.After(snailTime) { return game.Snails[0].Pos }
	return game.Snails[0].Pos + int((snailSpeed*float64(t.Sub(snailTime))) / float64(time.Second))
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
		return true  // Trigger periodic outputs even if nothing is happening
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
		p.Respawn()  // Drop berry, get off snail, etc.
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
		if !game.InGame() { return false }
		data := msg.Val.(parser.PickUpBerryMessage)
		game.Players[data.Player.Index()].HasBerry = true
	case "useMaiden":
		if !game.InGame() { return false }
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
				if p.HasSpeed { game.BlueTeam.SpeedWarriors++ }
			case GoldSide:
				game.GoldTeam.Warriors++
				if p.HasSpeed { game.GoldTeam.SpeedWarriors++ }
			}
		}
	case "blessMaiden":
		// TODO: Might be able to tag speed gates before gamestart on day.
		if !game.InGame() { return false }
		data := msg.Val.(parser.ClaimGateMessage)
		i := gateMap[data.Pos].index
		switch gateMap[data.Pos].typ {
		case SpeedGate: game.SpeedGates[i].ClaimedBy = data.Side
		case WarriorGate: game.WarriorGates[i].ClaimedBy = data.Side
		}
	case "playerKill":
		// TODO: Maybe can have kills before gamestart on trap map due to missing barriers
		if !game.InGame() { return false }
		data := msg.Val.(parser.PlayerKillMessage)
		v := &game.Players[data.Victim.Index()]
		switch v.Type {
		case Warrior:
			switch data.Victim.Team() {
			case BlueSide:
				game.BlueTeam.Warriors--
				if v.HasSpeed { game.BlueTeam.SpeedWarriors-- }
			case GoldSide:
				game.GoldTeam.Warriors--
				if v.HasSpeed { game.GoldTeam.SpeedWarriors-- }
			}
		case Queen:
			switch data.Victim.Team() {
			case BlueSide:
				game.BlueTeam.QueenDeaths++
			case GoldSide:
				game.GoldTeam.QueenDeaths++
			}
		case Drone, Robot: break
		}
		v.Respawn()
		if game.Players[data.Killer.Index()].OnSnail == 1 {
			snailTime = msg.Time  // Position set at start of eating.
		}
	case "getOnSnail: ":
		if !game.InGame() { return false }
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
			snailSpeed = 28.209890875  // 27
		} else {
			fmt.Fprintln(snailDebug, "rider is slow")
			snailSpeed = 20.896215463  // 20
		}
		if data.Rider.Team() == BlueSide {
			snailSpeed = -snailSpeed
		}
		game.Snails[0].Pos = pos
		snailTime = msg.Time
	case "getOffSnail: ":
		if !game.InGame() { return false }
		data := msg.Val.(parser.GetOffSnailMessage)
		game.Players[data.Rider.Index()].OnSnail = 0
		fmt.Fprintf(snailDebug, "Off: The snail moved by %v pixels in %v, %v px/s\n",
				   data.Pos.X - game.Snails[0].MaxPos - game.Snails[0].Pos,
				   msg.Time.Sub(snailTime),
				   float64(data.Pos.X - game.Snails[0].MaxPos - game.Snails[0].Pos) /
				   	   float64(msg.Time.Sub(snailTime)/time.Millisecond) * 1000)
		fmt.Fprintf(snailDebug, "Estimated snail position is %v, actual is %v, diff %v\n",
				   snailEstimate(msg.Time, game), data.Pos.X - game.Snails[0].MaxPos, data.Pos.X - game.Snails[0].MaxPos - snailEstimate(msg.Time, game))
		game.Snails[0].Pos = data.Pos.X - game.Snails[0].MaxPos
		snailTime = msg.Time
		snailSpeed = 0
	case "snailEat":
		if !game.InGame() { return false }
		data := msg.Val.(parser.SnailStartEatMessage)
		fmt.Fprintf(snailDebug, "Eat: The snail moved by %v pixels in %v, %v px/s\n",
				   data.Pos.X - game.Snails[0].MaxPos - game.Snails[0].Pos,
				   msg.Time.Sub(snailTime),
				   float64(data.Pos.X - game.Snails[0].MaxPos - game.Snails[0].Pos) /
				   	   float64(msg.Time.Sub(snailTime)/time.Millisecond) * 1000)
		fmt.Fprintf(snailDebug, "Estimated snail position is %v, actual is %v, diff %v\n",
				   snailEstimate(msg.Time, game), data.Pos.X - game.Snails[0].MaxPos, data.Pos.X - game.Snails[0].MaxPos - snailEstimate(msg.Time, game))
		game.Snails[0].Pos = data.Pos.X - game.Snails[0].MaxPos
		snailTime = msg.Time.Add(3500 * time.Millisecond)
	case "snailEscape":
		if !game.InGame() { return false }
		data := msg.Val.(parser.SnailEscapeEatMessage)
		// The escape event occurs at the snail's mouth, 50 pixels from it's position.
		var offset int
		if data.Escapee.Team() == BlueSide { offset = -50 } else { offset = 50 }
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
		if !game.InGame() { return false }
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
		if !game.InGame() { return false }
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

type CsvBuilder struct{
	strings.Builder
}
func (this *CsvBuilder) Append(items ...interface{}) {
	for _, it := range items {
		if this.Len() != 0 { this.WriteRune(',') }
		fmt.Fprint(this, it)
	}
}

const CsvHeader = "map,time_millis,gold_queen_type,gold_queen_speed,gold_queen_berry,gold_queen_snail,blue_queen_type,blue_queen_speed,blue_queen_berry,blue_queen_snail,gold_stripes_type,gold_stripes_speed,gold_stripes_berry,gold_stripes_snail,blue_stripes_type,blue_stripes_speed,blue_stripes_berry,blue_stripes_snail,gold_abs_type,gold_abs_speed,gold_abs_berry,gold_abs_snail,blue_abs_type,blue_abs_speed,blue_abs_berry,blue_abs_snail,gold_skulls_type,gold_skulls_speed,gold_skulls_berry,gold_skulls_snail,blue_skulls_type,blue_skulls_speed,blue_skulls_berry,blue_skulls_snail,gold_checks_type,gold_checks_speed,gold_checks_berry,gold_checks_snail,blue_checks_type,blue_checks_speed,blue_checks_berry,blue_checks_snail,gold_warriors,gold_queen_deaths,gold_berries,blue_warriors,blue_queen_deaths,blue_berries,snail_pos_last,snail_pos_estimate,snail_owner,snail_has_speed,warrior_gate_owner0,warrior_gate_owner1,warrior_gate_owner2,speed_gate_owner0,speed_gate_owner1,winner,end_condition"
type CsvPrinter struct{
	Map Map
	Duration time.Duration
	Time time.Time
	State kq.GameState
}
func (this *CsvPrinter) String() string {
	var b CsvBuilder
	b.Append(this.Map, int64(this.Duration/time.Millisecond))
	var rider PlayerId
	for i, p := range this.State.Players {
		b.Append(p.Type, p.HasSpeed, p.HasBerry, p.IsOnSnail())
		if p.IsOnSnail() { rider = PlayerId(i + 1) }
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
	berryWeight = 100. / 12.
	snailWeight = 200.
	lifeWeight = 100. / 3.
	warriorWeight = 100. / 4.
	speedWarriorWeight = warriorWeight * 4./3.  // Matches a queen right now.

	berryBonus1At, berryBonus1Rate, berryBonus1 = 4,    1.,   25
	berryBonus2At, berryBonus2Rate, berryBonus2 = 8,    1.,   50
	berryBonus3At, berryBonus3Rate, berryBonus3 = 10,   1.5,  100
	berryBonus4At, berryBonus4Rate, berryBonus4 = 11,   3.,   250
	snailBonusMinEligible = 0.55
	snailBonus1At, snailBonus1Rate, snailBonus1 = 0.7,  25,  25
	snailBonus2At, snailBonus2Rate, snailBonus2 = 0.8,  25,  50
	snailBonus3At, snailBonus3Rate, snailBonus3 = 0.9,  50,  100
	snailBonus4At, snailBonus4Rate, snailBonus4 = 0.95, 100, 200
	warriorBonus1At, warriorBonus1 = 2, 100
	queenBonus1At, queenBonus1 = 2, 100

	objectiveBonusFactorAtFullMil = 0.5
	faminePeakWarriorWeightFactor = 2.5
	famineDuration = 3 * time.Minute

	winPointsWeight, losePointsWeight = 1, 1
)

func logistic(value, half_amplitude, rate, center float64) float64 {
	return half_amplitude * 2 / (1 + math.Exp(rate * (center - value)))
}

func modelSumLose(game *kq.GameState, when time.Time) float64 {
	snailEst := snailEstimate(when, game)
	meta := maps.MetadataForMap(game.Map)
	snailLim := (meta.Snails[0].Nets[1].X - meta.Snails[0].Nets[0].X) / 2
	maxBerries := meta.BerriesAvailable
	queenStartLives := meta.QueenLives

	snailPos := (float64(snailEst) / float64(snailLim) + 1) / 2
	if snailPos < -1 { snailPos = -1 } else if snailPos > 1 { snailPos = 1 }
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
				1 + logistic(progress, (faminePeakWarriorWeightFactor - 1.) / 2., -30, 0.85)
		}
	} else {
		progress := float64(game.BerriesUsed) / float64(maxBerries)
		famineBerryBonusFactor = 1
		famineWarriorWeightFactor = 1
		if progress > 0.6 {
			famineBerryBonusFactor = logistic(progress, 0.5, -30, 0.85)
			famineWarriorWeightFactor =
				1 + logistic(progress, (faminePeakWarriorWeightFactor - 1.) / 2., 30, 0.85)
		}
	}

	var blueWin, blueLose, goldWin, goldLose float64
	blueLose += 100 - float64(game.GoldTeam.BerriesIn) * berryWeight
	goldLose += 100 - float64(game.BlueTeam.BerriesIn) * berryWeight
	blueLose += 100 - float64(game.BlueTeam.QueenDeaths) * lifeWeight
	goldLose += 100 - float64(game.GoldTeam.QueenDeaths) * lifeWeight
	blueWin += (
			float64(game.BlueTeam.Warriors-game.BlueTeam.SpeedWarriors) * warriorWeight +
			float64(game.BlueTeam.SpeedWarriors) * speedWarriorWeight) *
		famineWarriorWeightFactor
	goldWin += (
			float64(game.GoldTeam.Warriors-game.GoldTeam.SpeedWarriors) * warriorWeight +
			float64(game.GoldTeam.SpeedWarriors) * speedWarriorWeight) *
		famineWarriorWeightFactor
	if goldSnail <= 0.5 { blueLose += snailWeight / 2 } else { blueLose += (1 - goldSnail) * snailWeight }
	if blueSnail <= 0.5 { goldLose += snailWeight / 2 } else { goldLose += (1 - blueSnail) * snailWeight }

	blueFullMilFactor, goldFullMilFactor := 1., 1.
	if game.BlueTeam.Warriors == 4 { blueFullMilFactor = objectiveBonusFactorAtFullMil }
	if game.GoldTeam.Warriors == 4 { goldFullMilFactor = objectiveBonusFactorAtFullMil }

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
	if game.BlueTeam.Warriors - game.GoldTeam.Warriors >= warriorBonus1At {
		blueWin += warriorBonus1
	}
	if game.GoldTeam.Warriors - game.BlueTeam.Warriors >= warriorBonus1At {
		goldWin += warriorBonus1
	}
	if queenStartLives - game.BlueTeam.QueenDeaths >= queenBonus1At { blueWin += queenBonus1 }
	if queenStartLives - game.GoldTeam.QueenDeaths >= queenBonus1At { goldWin += queenBonus1 }

	blue := blueWin * winPointsWeight + blueLose * losePointsWeight
	gold := goldWin * winPointsWeight + goldLose * losePointsWeight

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

	snailPos := (float64(snailEst) / float64(snailLim) + 1) / 2
	if snailPos < -1 { snailPos = -1 } else if snailPos > 1 { snailPos = 1 }
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
				1 + logistic(progress, (faminePeakWarriorWeightFactor - 1.) / 2., -30, 0.85)
		}
	} else {
		progress := float64(game.BerriesUsed) / float64(maxBerries)
		famineBerryBonusFactor = 1
		famineWarriorWeightFactor = 1
		if progress > 0.6 {
			famineBerryBonusFactor = logistic(progress, 0.5, -30, 0.85)
			famineWarriorWeightFactor =
				1 + logistic(progress, (faminePeakWarriorWeightFactor - 1.) / 2., 30, 0.85)
		}
	}

	blueWin, blueLose, goldWin, goldLose := 0., 1., 0., 1.
	blueLose *= 1 - float64(game.GoldTeam.BerriesIn) * (berryWeight/100)
	goldLose *= 1 - float64(game.BlueTeam.BerriesIn) * (berryWeight/100)
	blueLose *= 1 - float64(game.BlueTeam.QueenDeaths) * (lifeWeight/100)
	goldLose *= 1 - float64(game.GoldTeam.QueenDeaths) * (lifeWeight/100)
	blueWin += (
			float64(game.BlueTeam.Warriors-game.BlueTeam.SpeedWarriors) * warriorWeight +
			float64(game.BlueTeam.SpeedWarriors) * speedWarriorWeight) *
		famineWarriorWeightFactor
	goldWin += (
			float64(game.GoldTeam.Warriors-game.GoldTeam.SpeedWarriors) * warriorWeight +
			float64(game.GoldTeam.SpeedWarriors) * speedWarriorWeight) *
		famineWarriorWeightFactor
	if goldSnail <= 0.5 { blueLose *= 1 } else { blueLose *= (1 - goldSnail) * (snailWeight/100) }
	if blueSnail <= 0.5 { goldLose *= 1 } else { goldLose *= (1 - blueSnail) * (snailWeight/100) }

	blueFullMilFactor, goldFullMilFactor := 1., 1.
	if game.BlueTeam.Warriors == 4 { blueFullMilFactor = objectiveBonusFactorAtFullMil }
	if game.GoldTeam.Warriors == 4 { goldFullMilFactor = objectiveBonusFactorAtFullMil }

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
	if game.BlueTeam.Warriors - game.GoldTeam.Warriors >= warriorBonus1At {
		blueWin += warriorBonus1
	}
	if game.GoldTeam.Warriors - game.BlueTeam.Warriors >= warriorBonus1At {
		goldWin += warriorBonus1
	}
	if queenStartLives - game.BlueTeam.QueenDeaths >= queenBonus1At { blueWin += queenBonus1 }
	if queenStartLives - game.GoldTeam.QueenDeaths >= queenBonus1At { goldWin += queenBonus1 }

	const myLosePointsWeight = 500
	blue := blueWin * winPointsWeight + blueLose * myLosePointsWeight
	gold := goldWin * winPointsWeight + goldLose * myLosePointsWeight

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

	snailPos := (float64(snailEst) / float64(snailLim) + 1) / 2
	if snailPos < -1 { snailPos = -1 } else if snailPos > 1 { snailPos = 1 }
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
				1 + logistic(progress, (faminePeakWarriorWeightFactor - 1.) / 2., -30, 0.85)
		}
	} else {
		progress := float64(game.BerriesUsed) / float64(maxBerries)
		famineBerryBonusFactor = 1
		famineWarriorWeightFactor = 1
		if progress > 0.6 {
			famineBerryBonusFactor = logistic(progress, 0.5, -30, 0.85)
			famineWarriorWeightFactor =
				1 + logistic(progress, (faminePeakWarriorWeightFactor - 1.) / 2., 30, 0.85)
		}
	}

	blueWin, blueLose, goldWin, goldLose := 0., 1., 0., 1.
	blueLose *= 1 - float64(game.GoldTeam.BerriesIn) * (berryWeight/100)
	goldLose *= 1 - float64(game.BlueTeam.BerriesIn) * (berryWeight/100)
	blueLose *= 1 - float64(game.BlueTeam.QueenDeaths) * (lifeWeight/100)
	goldLose *= 1 - float64(game.GoldTeam.QueenDeaths) * (lifeWeight/100)
	blueWin += (
			float64(game.BlueTeam.Warriors-game.BlueTeam.SpeedWarriors) * warriorWeight +
			float64(game.BlueTeam.SpeedWarriors) * speedWarriorWeight) *
		famineWarriorWeightFactor
	goldWin += (
			float64(game.GoldTeam.Warriors-game.GoldTeam.SpeedWarriors) * warriorWeight +
			float64(game.GoldTeam.SpeedWarriors) * speedWarriorWeight) *
		famineWarriorWeightFactor
	if goldSnail <= 0.5 { blueLose *= 1 } else { blueLose *= (1 - goldSnail) * (snailWeight/100) }
	if blueSnail <= 0.5 { goldLose *= 1 } else { goldLose *= (1 - blueSnail) * (snailWeight/100) }

	blueFullMilFactor, goldFullMilFactor := 1., 1.
	if game.BlueTeam.Warriors == 4 { blueFullMilFactor = objectiveBonusFactorAtFullMil }
	if game.GoldTeam.Warriors == 4 { goldFullMilFactor = objectiveBonusFactorAtFullMil }

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
	if game.BlueTeam.Warriors - game.GoldTeam.Warriors >= warriorBonus1At {
		blueWin += warriorBonus1
	}
	if game.GoldTeam.Warriors - game.BlueTeam.Warriors >= warriorBonus1At {
		goldWin += warriorBonus1
	}
	if queenStartLives - game.BlueTeam.QueenDeaths >= queenBonus1At { blueWin += queenBonus1 }
	if queenStartLives - game.GoldTeam.QueenDeaths >= queenBonus1At { goldWin += queenBonus1 }

	const myLosePointsWeight = 500
	blue := blueWin * winPointsWeight + math.Cbrt(blueLose) * myLosePointsWeight
	gold := goldWin * winPointsWeight + math.Cbrt(goldLose) * myLosePointsWeight

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

	snailPos := (float64(snailEst) / float64(snailLim) + 1) / 2
	if snailPos < -1 { snailPos = -1 } else if snailPos > 1 { snailPos = 1 }
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
				1 + logistic(progress, (faminePeakWarriorWeightFactor - 1.) / 2., -30, 0.85)
		}
	} else {
		progress := float64(game.BerriesUsed) / float64(maxBerries)
		famineBerryBonusFactor = 1
		famineWarriorWeightFactor = 1
		if progress > 0.6 {
			famineBerryBonusFactor = logistic(progress, 0.5, -30, 0.85)
			famineWarriorWeightFactor =
				1 + logistic(progress, (faminePeakWarriorWeightFactor - 1.) / 2., 30, 0.85)
		}
	}

	blueWin, blueLose, goldWin, goldLose := 0., 1., 0., 1.
	blueLose *= 1 - float64(game.GoldTeam.BerriesIn) * (berryWeight/100)
	goldLose *= 1 - float64(game.BlueTeam.BerriesIn) * (berryWeight/100)
	blueLose *= 1 - float64(game.BlueTeam.QueenDeaths) * (lifeWeight/100)
	goldLose *= 1 - float64(game.GoldTeam.QueenDeaths) * (lifeWeight/100)
	blueWin += (
			float64(game.BlueTeam.Warriors-game.BlueTeam.SpeedWarriors) * warriorWeight +
			float64(game.BlueTeam.SpeedWarriors) * speedWarriorWeight) *
		famineWarriorWeightFactor
	goldWin += (
			float64(game.GoldTeam.Warriors-game.GoldTeam.SpeedWarriors) * warriorWeight +
			float64(game.GoldTeam.SpeedWarriors) * speedWarriorWeight) *
		famineWarriorWeightFactor
	if goldSnail <= 0.5 { blueLose *= 1 } else { blueLose *= (1 - goldSnail) * (snailWeight/100) }
	if blueSnail <= 0.5 { goldLose *= 1 } else { goldLose *= (1 - blueSnail) * (snailWeight/100) }

	blueFullMilFactor, goldFullMilFactor := 1., 1.
	if game.BlueTeam.Warriors == 4 { blueFullMilFactor = objectiveBonusFactorAtFullMil }
	if game.GoldTeam.Warriors == 4 { goldFullMilFactor = objectiveBonusFactorAtFullMil }

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
	if game.BlueTeam.Warriors - game.GoldTeam.Warriors >= warriorBonus1At {
		blueWin += warriorBonus1
	}
	if game.GoldTeam.Warriors - game.BlueTeam.Warriors >= warriorBonus1At {
		goldWin += warriorBonus1
	}
	if queenStartLives - game.BlueTeam.QueenDeaths >= queenBonus1At { blueWin += queenBonus1 }
	if queenStartLives - game.GoldTeam.QueenDeaths >= queenBonus1At { goldWin += queenBonus1 }

	const myLosePointsWeight = 500
	blue := blueWin * winPointsWeight + math.Sqrt(blueLose) * myLosePointsWeight
	gold := goldWin * winPointsWeight + math.Sqrt(goldLose) * myLosePointsWeight

	total := blue + gold
	if teamSidesSwapped {
		return blue / total
	} else {
		return gold / total
	}
}

type autoConnector struct {
	conn *kqio.CabConnection
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

type teeReader struct {
	kqio.MessageStringReadWriteCloser
	w kqio.MessageStringWriter
}
func (t *teeReader) ReadMessageString(out *kqio.MessageString) error {
	if e := t.MessageStringReadWriteCloser.ReadMessageString(out); e != nil {
		return e
	}
	if e := t.w.WriteMessageString(out); e != nil { return e }
	return nil
}

func main() {
	broadcast := make(chan interface{})
	go startWebServer(broadcast)
	<-time.After(5 * time.Second)
	webStartTime, _ := time.Parse(time.RFC3339Nano, "2018-10-20T18:39:49.376-05:00")
	t := webStartTime

	var e error
	var strReader kqio.MessageStringReader
	var closer = func() {}
	if len(os.Args) >= 2 && len(os.Args[1]) > 0 {
		autoconn := &autoConnector{nil, func() (*kqio.CabConnection, error) {
			fmt.Fprintln(logOut, "Attempting to connect to", os.Args[1])
			return kqio.Connect(os.Args[1])
		}}
		replayLog, e = os.Create("out.log")
		if e != nil { panic(e) }
		strReader = &teeReader{autoconn, kqio.NewMessageStringWriter(replayLog)}
		closer = func() { fmt.Fprintln(logOut, "Disconnecting"); autoconn.Close() }
	} else {
		f, e := os.Open("../libkq/examples/BB3/red.logs-1540028543.54784.log")
		if e != nil { panic(e) }
		strReader = kqio.NewMessageStringReader(f)
	}
	defer closer()
	score := modelSumLose
	scorers := [...]func(*kq.GameState, time.Time) float64{modelSumLose, modelMultLose, modelMultCbrt, modelMultSqrt}
	if len(os.Args) >= 3 {
		switch os.Args[2] {
		case "--model=sumLose": score = modelSumLose
		case "--model=multLose": score = modelMultLose
		case "--model=multCbrt": score = modelMultCbrt
		case "--model=multSqrt": score = modelMultSqrt
		default: panic(fmt.Sprintf("Unknown argument %v", os.Args[2]))
		}
	}
	reader := kq.NewCabinet(strReader)
	var msg kqio.Message
	state := &kq.GameState{}
	csvOut, e = os.Create("out.csv")
	if e != nil { panic(e) }
	fmt.Fprintln(csvOut, CsvHeader)
	for {
		e = reader.ReadMessage(&msg)
		if e != nil {
			fmt.Fprintln(msgDump, "read error", e)
			if e == io.EOF { break }
			continue
		}
		fmt.Fprintln(msgDump, msg)
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
				for _, scorer := range scorers { dp.vals = append(dp.vals, scorer(state, msg.Time)) }
				broadcast <- dp
			}
			if s <= 0.5 {
				fmt.Fprintf(predictionOut, "%*s%*v%%\n",
							int(s * 80), "|",
							41 - int(s*80), int((0.5 - s) * 200))
			} else {
				fmt.Fprintf(predictionOut, "%38v%%%*s\n",
							int((s - 0.5) * 200),
							int(s * 80) - 39, "|")
			}
			t = msg.Time
		}
	}

	_ = t
/*
	s := 0.5
	i := 0
	for _ = range time.Tick(time.Second) {
		t = t.Add(time.Second)
		if i % 20 == 0 { broadcast <- t }
		i++
		broadcast <- dataPoint{t, s}
		r := rand.Float64() * 1.1 - 0.6 // Even over range [-0.6, 0.5).
		if s < 0.5 {
			s -= r * s
		} else if s > 0.5 {
			s += r * (1 - s)
		} else {
			s += r / 2
		}
	}
*/
}
