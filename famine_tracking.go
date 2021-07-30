package main

import (
	"time"

	kq "github.com/ughoavgfhw/libkq"
	"github.com/ughoavgfhw/libkq/maps"
)

type famineUpdateEventKey int

var FamineUpdateKey famineUpdateEventKey

type FamineUpdate struct {
	BerriesLeft int
	FamineStart time.Time
	CurrTime    time.Time
}

type FamineTracker struct {
	berryCount int
}

func NewFamineTracker() *FamineTracker {
	return &FamineTracker{0}
}

func (ft *FamineTracker) Update(event *Event, state *kq.GameState) {
	mapData := maps.MetadataForMap(state.Map)
	if mapData == nil {
		return
	}
	berries := mapData.BerriesAvailable - state.BerriesUsed
	if berries != ft.berryCount || (state.InFamine() && event.IsTick) {
		ft.berryCount = berries
		event.Data[FamineUpdateKey] = FamineUpdate{berries, state.FamineStart, event.When}
	}
}
