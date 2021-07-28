package main

import (
	"math"
	"time"

	kq "github.com/ughoavgfhw/libkq"
	"github.com/ughoavgfhw/libkq/maps"
)

type StateScorer func(*kq.GameState, time.Time) float64

func AllStateScores(state *kq.GameState, when time.Time) []float64 {
	return []float64{
		modelSumLose(state, when),
		modelMultLose(state, when),
		modelMultCbrt(state, when),
		modelMultSqrt(state, when),
		modelMultQueenSqrt(state, when),
	}
}

func GetStateScorerByName(name string) StateScorer {
	switch name {
	case "sumLose":
		return modelSumLose
	case "multLose":
		return modelMultLose
	case "multCbrt":
		return modelMultCbrt
	case "multSqrt":
		return modelMultSqrt
	case "multQSqrt":
		return modelMultQueenSqrt
	default:
		return nil
	}
}

// -----------------------------
// Models implemented below. WARNING: This code is a mess.

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
