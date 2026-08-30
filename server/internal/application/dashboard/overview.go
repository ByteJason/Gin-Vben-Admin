package dashboard

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

// DataSource tells clients whether operational analytics came from persisted
// collectors or the deterministic development fixture.  The marker is part of
// the response so a fixture can never be mistaken for production telemetry.
type DataSource string

const (
	DataSourceLive    DataSource = "live"
	DataSourceFixture DataSource = "fixture"
)

type Preset string

const (
	PresetToday     Preset = "today"
	PresetYesterday Preset = "yesterday"
	Preset24Hours   Preset = "24h"
	Preset7Days     Preset = "7d"
	Preset14Days    Preset = "14d"
	Preset30Days    Preset = "30d"
	PresetThisMonth Preset = "this_month"
	PresetLastMonth Preset = "last_month"
	PresetCustom    Preset = "custom"
)

const maxCustomRange = 90 * 24 * time.Hour

var ErrInvalidOverviewQuery = errors.New("invalid dashboard overview query")

// OverviewQuery uses a half-open interval [From, To).  Timezone must be an
// IANA name; From and To are interpreted as instants (or as local dates when
// parsed by the HTTP transport).
type OverviewQuery struct {
	Preset      Preset
	Timezone    string
	From        *time.Time
	To          *time.Time
	Granularity string
}

type TimeRange struct {
	Preset      Preset    `json:"preset"`
	Timezone    string    `json:"timezone"`
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
	Granularity string    `json:"granularity"`
}

type NumberMetric struct {
	Status  Status   `json:"status"`
	Value   *float64 `json:"value,omitempty"`
	Message string   `json:"message,omitempty"`
}

type OverviewCards struct {
	Visitors         NumberMetric `json:"visitors"`
	NewUsers         NumberMetric `json:"newUsers"`
	PaymentAmount    NumberMetric `json:"paymentAmount"`
	PaymentOrders    NumberMetric `json:"paymentOrders"`
	AverageOrderSize NumberMetric `json:"averageOrderValue"`
}

type TrendPoint struct {
	At       time.Time `json:"at"`
	Visitors float64   `json:"visitors"`
	NewUsers float64   `json:"newUsers"`
	Orders   float64   `json:"orders"`
	Amount   float64   `json:"amount"`
}

type DistributionItem struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

type TopItem struct {
	Rank   int     `json:"rank"`
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Value  float64 `json:"value"`
	Amount float64 `json:"amount"`
}

type Announcement struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	PublishedAt time.Time `json:"publishedAt"`
}

// Overview is deliberately an operations projection rather than a billing
// ledger. Live sources may legitimately return empty analytic collections.
type Overview struct {
	DataSource    DataSource         `json:"dataSource"`
	IsSynthetic   bool               `json:"isSynthetic"`
	Range         TimeRange          `json:"range"`
	Cards         OverviewCards      `json:"cards"`
	Trends        []TrendPoint       `json:"trends"`
	Distribution  []DistributionItem `json:"distribution"`
	TopItems      []TopItem          `json:"topItems"`
	Regions       []DistributionItem `json:"regions"`
	Announcements []Announcement     `json:"announcements"`
	CollectedAt   time.Time          `json:"collectedAt"`
}

// Overview returns tenant-scoped dashboard data. Fixture data is generated
// only from the normalized requested range, making it repeatable for tests and
// development while keeping live data tied to the existing scoped collectors.
func (s *Service) Overview(ctx context.Context, query OverviewQuery) (Overview, error) {
	if _, err := tenant.RequireContext(ctx); err != nil {
		return Overview{}, err
	}
	rangeValue, err := s.resolveOverviewRange(query)
	if err != nil {
		return Overview{}, err
	}
	if s.dataSource() == DataSourceFixture {
		return fixtureOverview(rangeValue, s.clock().UTC()), nil
	}
	return s.liveOverview(ctx, rangeValue)
}

func (s *Service) dataSource() DataSource {
	if s.config.DataSource == DataSourceFixture {
		return DataSourceFixture
	}
	return DataSourceLive
}

func (s *Service) resolveOverviewRange(query OverviewQuery) (TimeRange, error) {
	preset := query.Preset
	if preset == "" {
		preset = PresetToday
	}
	timezone := strings.TrimSpace(query.Timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return TimeRange{}, fmt.Errorf("%w: timezone", ErrInvalidOverviewQuery)
	}
	now := s.clock().In(location)
	startDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	granularity := strings.TrimSpace(query.Granularity)
	if granularity == "" {
		granularity = defaultGranularity(preset)
	}
	if granularity != "hour" && granularity != "day" {
		return TimeRange{}, fmt.Errorf("%w: granularity", ErrInvalidOverviewQuery)
	}
	rangeValue := TimeRange{Preset: preset, Timezone: timezone, Granularity: granularity}
	switch preset {
	case PresetToday:
		rangeValue.From, rangeValue.To = startDay, startDay.AddDate(0, 0, 1)
	case PresetYesterday:
		rangeValue.To = startDay
		rangeValue.From = startDay.AddDate(0, 0, -1)
	case Preset24Hours:
		rangeValue.To = now
		rangeValue.From = now.Add(-24 * time.Hour)
	case Preset7Days, Preset14Days, Preset30Days:
		days := map[Preset]int{Preset7Days: 7, Preset14Days: 14, Preset30Days: 30}[preset]
		rangeValue.To = now
		rangeValue.From = now.Add(-time.Duration(days) * 24 * time.Hour)
	case PresetThisMonth:
		rangeValue.From = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, location)
		rangeValue.To = rangeValue.From.AddDate(0, 1, 0)
	case PresetLastMonth:
		rangeValue.To = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, location)
		rangeValue.From = rangeValue.To.AddDate(0, -1, 0)
	case PresetCustom:
		if query.From == nil || query.To == nil {
			return TimeRange{}, fmt.Errorf("%w: custom range", ErrInvalidOverviewQuery)
		}
		rangeValue.From = query.From.In(location)
		rangeValue.To = query.To.In(location)
		if !rangeValue.From.Before(rangeValue.To) || rangeValue.To.Sub(rangeValue.From) > maxCustomRange {
			return TimeRange{}, fmt.Errorf("%w: custom range", ErrInvalidOverviewQuery)
		}
	default:
		return TimeRange{}, fmt.Errorf("%w: preset", ErrInvalidOverviewQuery)
	}
	return rangeValue, nil
}

func defaultGranularity(preset Preset) string {
	if preset == Preset24Hours || preset == PresetToday || preset == PresetYesterday {
		return "hour"
	}
	return "day"
}

func (s *Service) liveOverview(ctx context.Context, rangeValue TimeRange) (Overview, error) {
	summary, err := s.Summary(ctx)
	if err != nil {
		return Overview{}, err
	}
	users := unavailableNumberMetric()
	if summary.Counts.Users.Value != nil {
		users = NumberMetric{Status: summary.Counts.Users.Status, Value: floatPointer(float64(*summary.Counts.Users.Value)), Message: summary.Counts.Users.Message}
	} else {
		users.Status, users.Message = summary.Counts.Users.Status, summary.Counts.Users.Message
	}
	return Overview{
		DataSource: DataSourceLive, IsSynthetic: false, Range: rangeValue,
		Cards:  OverviewCards{Visitors: unavailableNumberMetric(), NewUsers: users, PaymentAmount: unavailableNumberMetric(), PaymentOrders: unavailableNumberMetric(), AverageOrderSize: unavailableNumberMetric()},
		Trends: []TrendPoint{}, Distribution: []DistributionItem{}, TopItems: []TopItem{}, Regions: []DistributionItem{}, Announcements: []Announcement{}, CollectedAt: summary.CollectedAt,
	}, nil
}

func unavailableNumberMetric() NumberMetric {
	return NumberMetric{Status: StatusUnavailable, Message: "collector not configured"}
}

func fixtureOverview(rangeValue TimeRange, collectedAt time.Time) Overview {
	// The sequence is a pure function of the requested bucket positions, not of
	// wall-clock collection time. It is therefore stable across refreshes.
	step := 24 * time.Hour
	if rangeValue.Granularity == "hour" {
		step = time.Hour
	}
	points := make([]TrendPoint, 0)
	visitors, newUsers, orders, amount := 0.0, 0.0, 0.0, 0.0
	for at, index := rangeValue.From, 0; at.Before(rangeValue.To); at, index = addOverviewStep(at, step, rangeValue.Granularity), index+1 {
		v := float64(18 + (index*7)%23)
		n := float64(1 + (index*3)%6)
		o := float64(2 + (index*5)%9)
		a := o * float64(42+(index%5)*9)
		points = append(points, TrendPoint{At: at, Visitors: v, NewUsers: n, Orders: o, Amount: a})
		visitors, newUsers, orders, amount = visitors+v, newUsers+n, orders+o, amount+a
	}
	average := 0.0
	if orders > 0 {
		average = amount / orders
	}
	return Overview{
		DataSource: DataSourceFixture, IsSynthetic: true, Range: rangeValue,
		Cards:         OverviewCards{Visitors: fixtureMetric(visitors), NewUsers: fixtureMetric(newUsers), PaymentAmount: fixtureMetric(amount), PaymentOrders: fixtureMetric(orders), AverageOrderSize: fixtureMetric(average)},
		Trends:        points,
		Distribution:  []DistributionItem{{Key: "web", Label: "Web", Value: 56}, {Key: "mobile", Label: "Mobile", Value: 31}, {Key: "api", Label: "API", Value: 13}},
		TopItems:      []TopItem{{Rank: 1, ID: "fixture-basic", Name: "Fixture Basic", Value: 38, Amount: 2280}, {Rank: 2, ID: "fixture-pro", Name: "Fixture Pro", Value: 24, Amount: 2160}, {Rank: 3, ID: "fixture-team", Name: "Fixture Team", Value: 16, Amount: 1920}},
		Regions:       []DistributionItem{{Key: "apac", Label: "APAC", Value: 45}, {Key: "emea", Label: "EMEA", Value: 33}, {Key: "americas", Label: "Americas", Value: 22}},
		Announcements: []Announcement{{ID: "fixture-dashboard", Title: "Fixture data is enabled", PublishedAt: rangeValue.From}},
		CollectedAt:   collectedAt,
	}
}

func fixtureMetric(value float64) NumberMetric {
	return NumberMetric{Status: StatusOK, Value: floatPointer(value)}
}
func floatPointer(value float64) *float64 { return &value }

func addOverviewStep(at time.Time, step time.Duration, granularity string) time.Time {
	if granularity == "day" {
		return at.AddDate(0, 0, 1)
	}
	return at.Add(step)
}
