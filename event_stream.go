package main

import (
	"time"

	"github.com/ughoavgfhw/libkq/io"
)

type baseDataEventKey int

const (
	CabMessageKey baseDataEventKey = iota
	ControlCommandKey
)

type EventType int

const (
	InvalidEventType EventType = iota
	CabMessageEvent
	ControlEvent
)

type Event struct {
	When   time.Time
	Type   EventType
	Data   map[interface{}]interface{}
	IsTick bool
}

func EventWithMessage(msg *kqio.Message, tick bool) *Event {
	e := &Event{msg.Time, CabMessageEvent, make(map[interface{}]interface{}), tick}
	e.Data[CabMessageKey] = msg
	return e
}

func NewControlEvent(commands []ControlCommand) *Event {
	e := &Event{time.Now(), ControlEvent, make(map[interface{}]interface{}), false}
	e.Data[ControlCommandKey] = commands
	return e
}

type EventStream chan *Event

func NewEventStream() EventStream {
	return make(chan *Event, 8)
}

func (strm EventStream) Close() error {
	close(strm)
	return nil
}

// Adds an event to the stream. May block.
func (strm EventStream) AddEvent(event *Event) {
	strm <- event
}

// Reads an event from the stream. May block.
func (strm EventStream) Next() *Event {
	return <-strm
}
