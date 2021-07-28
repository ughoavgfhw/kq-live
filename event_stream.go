package main

import (
	"time"

	"github.com/ughoavgfhw/libkq/io"
)

type Event struct {
	When   time.Time
	IsTick bool
	Data   map[interface{}]interface{}
}

type cabMessageEventKey int

var CabMessageKey cabMessageEventKey

func EventWithMessage(msg *kqio.Message, tick bool) *Event {
	e := &Event{msg.Time, tick, make(map[interface{}]interface{})}
	e.Data[CabMessageKey] = msg
	return e
}
