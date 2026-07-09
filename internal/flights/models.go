package flights

// FlightDTO is the public flight contract, decoupled from the AviationStack
// upstream types so the provider can be swapped without breaking API consumers.
type FlightDTO struct {
	FlightDate   string      `json:"flightDate"`
	Status       string      `json:"status"` // scheduled|active|landed|cancelled|...
	Airline      string      `json:"airline,omitempty"`
	FlightNumber string      `json:"flightNumber,omitempty"` // IATA, e.g. "AI865"
	Departure    EndpointDTO `json:"departure"`
	Arrival      EndpointDTO `json:"arrival"`
	Live         *LiveDTO    `json:"live,omitempty"`
}

// EndpointDTO is a departure or arrival summary.
type EndpointDTO struct {
	Airport   string `json:"airport,omitempty"`
	IATA      string `json:"iata,omitempty"`
	Terminal  string `json:"terminal,omitempty"`
	Gate      string `json:"gate,omitempty"`
	Scheduled string `json:"scheduled,omitempty"`
	Estimated string `json:"estimated,omitempty"`
	Actual    string `json:"actual,omitempty"`
	DelayMin  *int   `json:"delayMin,omitempty"`
	Timezone  string `json:"timezone,omitempty"`
}

// LiveDTO is the real-time position for an in-air flight.
type LiveDTO struct {
	Updated   string  `json:"updated,omitempty"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Altitude  float64 `json:"altitude"`
	Direction float64 `json:"direction"`
	SpeedKmh  float64 `json:"speedKmh"`
	OnGround  bool    `json:"onGround"`
}
