package types

import "time"

type StatusInfo struct {
	Id                    uint64
	Fuel                  float64
	GpsX                  float64
	GpsY                  float64
	GpsZ                  float64
	PitchLookaheadSeconds float64
	TargetDir             float64
	TargetDist            float64
	VehicleSpeed          float64
	LastUpdate            time.Time
}
