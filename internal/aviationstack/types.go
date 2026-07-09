// Package aviationstack is a thin, typed client for the AviationStack flights
// API (https://aviationstack.com/documentation). Auth is via the `access_key`
// query parameter. It does not cache — that is the flights service's job.
package aviationstack

// flightsEnvelope is the top-level /flights response.
type flightsEnvelope struct {
	Pagination Pagination `json:"pagination"`
	Data       []Flight   `json:"data"`
	Error      *errBody   `json:"error"` // present (with HTTP 200) on some upstream errors
}

// Pagination is the paging block returned with every list response.
type Pagination struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Count  int `json:"count"`
	Total  int `json:"total"`
}

// Flight is one flight record.
type Flight struct {
	FlightDate   string     `json:"flight_date"`
	FlightStatus string     `json:"flight_status"` // scheduled|active|landed|cancelled|incident|diverted
	Departure    Endpoint   `json:"departure"`
	Arrival      Endpoint   `json:"arrival"`
	Airline      Airline    `json:"airline"`
	Flight       FlightMeta `json:"flight"`
	Live         *Live      `json:"live"`
}

// Endpoint is a departure or arrival block.
type Endpoint struct {
	Airport   string `json:"airport"`
	Timezone  string `json:"timezone"`
	IATA      string `json:"iata"`
	ICAO      string `json:"icao"`
	Terminal  string `json:"terminal"`
	Gate      string `json:"gate"`
	Delay     *int   `json:"delay"` // minutes, when known
	Scheduled string `json:"scheduled"`
	Estimated string `json:"estimated"`
	Actual    string `json:"actual"`
}

// Airline is the operating carrier.
type Airline struct {
	Name string `json:"name"`
	IATA string `json:"iata"`
	ICAO string `json:"icao"`
}

// FlightMeta is the flight number in its various codings.
type FlightMeta struct {
	Number string `json:"number"`
	IATA   string `json:"iata"` // e.g. "AI865"
	ICAO   string `json:"icao"`
}

// Live is the real-time position (present only for in-air flights on plans that
// include live tracking).
type Live struct {
	Updated         string  `json:"updated"`
	Latitude        float64 `json:"latitude"`
	Longitude       float64 `json:"longitude"`
	Altitude        float64 `json:"altitude"`
	Direction       float64 `json:"direction"`
	SpeedHorizontal float64 `json:"speed_horizontal"`
	SpeedVertical   float64 `json:"speed_vertical"`
	IsGround        bool    `json:"is_ground"`
}

// errBody is the AviationStack error object.
type errBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
