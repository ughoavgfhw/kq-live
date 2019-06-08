package main

import kq "github.com/ughoavgfhw/libkq/common"

type GameScore struct {
	TeamASide kq.Side
	Winner    kq.Side
	WinType   kq.WinType
}

type MatchScores struct {
	TeamA  string
	TeamB  string
	ScoreA int
	ScoreB int

	Games []*GameScore
}

type ActiveMatch struct {
	TeamASide kq.Side
	MatchVictoryRule

	*MatchScores
}

// Resets for the given match. If the match already has any games recorded,
// assumes team A is on the same side as the previous game. Otherwise assumes
// team A is on blue. Holds onto the passed-in scores.
func (m *ActiveMatch) Reset(scores *MatchScores) {
	m.MatchScores = scores
	if len(scores.Games) > 0 {
		m.TeamASide = scores.Games[len(scores.Games)-1].TeamASide
	} else {
		m.TeamASide = kq.BlueSide
	}
}

// Switches which team is on which side.
func (m *ActiveMatch) SwapSides() {
	switch m.TeamASide {
	case kq.BlueSide:
		m.TeamASide = kq.GoldSide
	case kq.GoldSide:
		m.TeamASide = kq.BlueSide
	default: // Uninitialized
	}
}

// Records the result of a game, updating scores and adding the game to the
// match history.
func (m *ActiveMatch) RecordGame(winner kq.Side, winType kq.WinType) {
	m.Games = append(m.Games, &GameScore{
		TeamASide: m.TeamASide,
		Winner:    winner,
		WinType:   winType,
	})
	if winner == m.TeamASide {
		m.ScoreA++
	} else {
		m.ScoreB++
	}
}

// Clears the previous game from a match, updating the scores as needed.
func (m *ActiveMatch) ClearPreviousGame() {
	last := len(m.Games) - 1
	if m.Games[last].TeamASide == m.Games[last].Winner {
		m.ScoreA--
	} else {
		m.ScoreB--
	}
	m.Games[last] = nil
	m.Games = m.Games[:last]
}

func (m *ActiveMatch) IsComplete() bool {
	return m.MatchVictoryRule.MatchIsComplete(m.ScoreA, m.ScoreB)
}

type UnstructuredPlay struct {
	current  ActiveMatch
	upcoming []*MatchScores
}

func StartUnstructuredPlay(victoryRule MatchVictoryRule) *UnstructuredPlay {
	p := new(UnstructuredPlay)
	p.current.Reset(new(MatchScores))
	p.current.MatchVictoryRule = victoryRule
	return p
}

// Returns the current match's scores. The caller may take ownership of the
// result; this is useful for tracking completed matches.
func (p *UnstructuredPlay) CurrentMatch() *MatchScores {
	return p.current.MatchScores
}

// Returns the scores object for an upcoming match. This can be used to set
// team names ahead of time. Upcoming matches will be moved to current status
// by AdvanceMatch(). At all times, `UpcomingMatch(0)` is next,
// `UpcomingMatch(1)` is after that, and so on.
func (p *UnstructuredPlay) UpcomingMatch(distance int) *MatchScores {
	for distance >= len(p.upcoming) {
		p.upcoming = append(p.upcoming, &MatchScores{})
	}
	return p.upcoming[distance]
}

// Finishes the current match and moves to the first upcoming match. If no
// upcoming matches have been set up, an empty match will be created. The
// scores for the previously-current match are discarded; the caller can take
// ownership of them by calling CurrentMatch() before advancing.
func (p *UnstructuredPlay) AdvanceMatch() {
	p.current.Reset(p.UpcomingMatch(0))
	last := len(p.upcoming) - 1
	copy(p.upcoming[:last], p.upcoming[1:])
	p.upcoming[last] = nil
	p.upcoming = p.upcoming[:last]
}

func (p *UnstructuredPlay) TeamASide() kq.Side {
	return p.current.TeamASide
}

func (p *UnstructuredPlay) SetTeamASide(side kq.Side) {
	p.current.TeamASide = side
}

// Switches which team is on which side.
func (p *UnstructuredPlay) SwapSides() {
	p.current.SwapSides()
}

// Records the result of a game in the current match, updating scores.
func (p *UnstructuredPlay) RecordGame(winner kq.Side, winType kq.WinType) {
	p.current.RecordGame(winner, winType)
}

// Clears the previous game from the current match match, updating the scores
// as needed.
func (p *UnstructuredPlay) ClearPreviousGame() {
	p.current.ClearPreviousGame()
}

func (p *UnstructuredPlay) VictoryRule() MatchVictoryRule {
	return p.current.MatchVictoryRule
}
func (p *UnstructuredPlay) SetVictoryRule(rule MatchVictoryRule) {
	p.current.MatchVictoryRule = rule
}
func (p *UnstructuredPlay) CurrentMatchIsComplete() bool {
	return p.current.IsComplete()
}

type MatchVictoryRule interface {
	MaxPossibleWins() int
	MatchIsComplete(scoreA, scoreB int) bool
}

type BestOfN int

func (bo BestOfN) MaxPossibleWins() int { return int(bo+1) / 2 }
func (bo BestOfN) MatchIsComplete(scoreA, scoreB int) bool {
	winsNeeded := bo.MaxPossibleWins()
	return scoreA >= winsNeeded || scoreB >= winsNeeded
}

type StraightN int

func (n StraightN) MaxPossibleWins() int { return int(n) }
func (n StraightN) MatchIsComplete(scoreA, scoreB int) bool {
	return scoreA+scoreB >= int(n)
}
