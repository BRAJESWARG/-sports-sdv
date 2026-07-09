// Package flights is the business layer for flight search: it orchestrates the
// AviationStack client + cache and maps upstream types to the public FlightDTO.
package flights

import (
	"context"
	"encoding/json"
	"net/url"
	"time"

	"github.com/bgmaster/sports-sdv/internal/aviationstack"
	"github.com/bgmaster/sports-sdv/internal/cache"
)

// Service wraps the AviationStack client with cache-aside. Caching matters: the
// free AviationStack plan is heavily request-limited, so identical searches must
// not re-hit the upstream within the TTL.
type Service struct {
	client *aviationstack.Client
	cache  cache.Cache
	ttl    time.Duration
}

// New wires the flights service.
func New(client *aviationstack.Client, c cache.Cache, ttl time.Duration) *Service {
	return &Service{client: client, cache: c, ttl: ttl}
}

// Search returns flights matching the upstream query filters (dep_iata, arr_iata,
// flight_iata, airline_iata, flight_date, flight_status, limit). The access_key
// is added inside the client, so it never appears in the cache key or logs.
func (s *Service) Search(ctx context.Context, q url.Values) ([]FlightDTO, error) {
	key := "flights?" + q.Encode()
	return cacheAside(ctx, s.cache, key, s.ttl, func(ctx context.Context) ([]FlightDTO, error) {
		fs, err := s.client.Flights(ctx, q)
		if err != nil {
			return nil, err
		}
		return mapFlights(fs), nil
	})
}

func mapFlights(fs []aviationstack.Flight) []FlightDTO {
	out := make([]FlightDTO, 0, len(fs))
	for _, f := range fs {
		out = append(out, mapFlight(f))
	}
	return out
}

func mapFlight(f aviationstack.Flight) FlightDTO {
	d := FlightDTO{
		FlightDate:   f.FlightDate,
		Status:       f.FlightStatus,
		Airline:      f.Airline.Name,
		FlightNumber: f.Flight.IATA,
		Departure:    mapEndpoint(f.Departure),
		Arrival:      mapEndpoint(f.Arrival),
	}
	if f.Live != nil {
		d.Live = &LiveDTO{
			Updated:   f.Live.Updated,
			Latitude:  f.Live.Latitude,
			Longitude: f.Live.Longitude,
			Altitude:  f.Live.Altitude,
			Direction: f.Live.Direction,
			SpeedKmh:  f.Live.SpeedHorizontal,
			OnGround:  f.Live.IsGround,
		}
	}
	return d
}

func mapEndpoint(e aviationstack.Endpoint) EndpointDTO {
	return EndpointDTO{
		Airport:   e.Airport,
		IATA:      e.IATA,
		Terminal:  e.Terminal,
		Gate:      e.Gate,
		Scheduled: e.Scheduled,
		Estimated: e.Estimated,
		Actual:    e.Actual,
		DelayMin:  e.Delay,
		Timezone:  e.Timezone,
	}
}

// cacheAside returns the cached value or loads, stores, and returns it.
func cacheAside[T any](ctx context.Context, c cache.Cache, key string, ttl time.Duration, load func(context.Context) (T, error)) (T, error) {
	var zero T
	if raw, ok := c.Get(key); ok {
		var v T
		if err := json.Unmarshal(raw, &v); err == nil {
			return v, nil
		}
	}
	v, err := load(ctx)
	if err != nil {
		return zero, err
	}
	if raw, err := json.Marshal(v); err == nil {
		c.Set(key, raw, ttl)
	}
	return v, nil
}
