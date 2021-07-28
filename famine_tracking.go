package main

import (
	"time"

	kq "github.com/ughoavgfhw/libkq"
	"github.com/ughoavgfhw/libkq/maps"
)

type FamineUpdate struct {
	BerriesLeft int
	FamineStart time.Time
	CurrTime    time.Time
}

type FamineTracker struct {
	berryCount int
	broadcast  chan<- interface{}
}

func NewFamineTracker(broadcast chan<- interface{}) *FamineTracker {
	return &FamineTracker{0, broadcast}
}

func (ft *FamineTracker) Update(when time.Time, state *kq.GameState, isTick bool) {
	mapData := maps.MetadataForMap(state.Map)
	if mapData == nil {
		return
	}
	berries := mapData.BerriesAvailable - state.BerriesUsed
	if berries != ft.berryCount || (state.InFamine() && isTick) {
		ft.berryCount = berries
		ft.broadcast <- FamineUpdate{berries, state.FamineStart, when}
	}
}
