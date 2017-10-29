package hook

import (
	"expvar"
)

var (
	announcesRcv = expvar.NewInt("dhtAnnounces")
)

//todo: add more vars
