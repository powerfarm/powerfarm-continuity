package ingress

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

// Defaults keep the configuration small without leaving any decision implicit
// in code that matters to an effect.
const (
	defaultMaxBodyBytes   int64 = 1 << 20
	defaultTimeoutSeconds       = 300
	defaultReviewBound          = 3
	defaultSweepSeconds         = 60
	defaultListen               = "127.0.0.1:8787"
)

// refusalExit is the exit status by which a route says it was refused for
// credential, quota or budget reasons rather than failing technically. It is
// the convention institution-turn already uses for its own routes, so a
// boundary is established by evidence instead of inferred from free text.
const refusalExit = 3

// Config is the operator's configuration of one ingress. It is trusted local
// configuration, not a Registry admission, and it is the only place where the
// meaning of a route's exit is declared.
type Config struct {
	Listen       string   `json:"listen,omitempty"`
	TokenFile    string   `json:"tokenFile"`
	State        string   `json:"state"`
	Report       []string `json:"report"`
	MaxBodyBytes int64    `json:"maxBodyBytes,omitempty"`
	ReviewBound  int      `json:"reviewBound,omitempty"`
	SweepSeconds int      `json:"sweepSeconds,omitempty"`
	Routes       []Route  `json:"routes"`
}

// Route is the executable relationship that already owns one kind of work. It
// is selected by the handoff each occurrence names, because Heartime states
// that execution belongs to the relationship in that reference and to nothing
// else. The ingress hands the activation to it and nothing more.
type Route struct {
	Name           string     `json:"name"`
	Handoff        string     `json:"handoff"`
	Generation     int        `json:"generation,omitempty"`
	Kinds          []string   `json:"kinds"`
	Argv           []string   `json:"argv"`
	TimeoutSeconds int        `json:"timeoutSeconds,omitempty"`
	Outcome        OutcomeMap `json:"outcome,omitempty"`
}

// OutcomeMap declares how this route's exit is read as execution feedback. The
// ingress never infers verification: if a route does not say how success is
// established, its activations resolve as uncertain.
//
// A route killed by timeout or signal is always uncertain, because effects may
// have happened. That is doctrine, not policy, and cannot be configured away.
type OutcomeMap struct {
	Field     string  `json:"field,omitempty"`
	WhenTrue  Outcome `json:"whenTrue,omitempty"`
	WhenFalse Outcome `json:"whenFalse,omitempty"`
	OnRefusal Outcome `json:"onRefusal,omitempty"`
	OnFailure Outcome `json:"onFailure,omitempty"`
	OnMissing Outcome `json:"onMissing,omitempty"`
}

// LoadConfig reads exactly one configuration document. Unknown fields are
// refused so that a misspelled term never silently keeps a default.
func LoadConfig(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()
	var config Config
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	return config, config.normalize()
}

// normalize applies the documented defaults and refuses anything the ingress
// would otherwise have to guess at runtime.
func (c *Config) normalize() error {
	invalid := func(format string, arguments ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidConfig, fmt.Sprintf(format, arguments...))
	}
	if c.Listen == "" {
		c.Listen = defaultListen
	}
	if c.TokenFile == "" {
		return invalid("tokenFile is required: an unauthenticated ingress accepts anyone's obligations")
	}
	if c.State == "" {
		return invalid("state is required: deduplication and recovery are local state")
	}
	if len(c.Report) == 0 {
		return invalid("report is required: without it an outcome can never reach Heartime")
	}
	if c.MaxBodyBytes <= 0 {
		c.MaxBodyBytes = defaultMaxBodyBytes
	}
	if c.ReviewBound <= 0 {
		c.ReviewBound = defaultReviewBound
	}
	if c.SweepSeconds <= 0 {
		c.SweepSeconds = defaultSweepSeconds
	}
	if len(c.Routes) == 0 {
		return invalid("at least one route is required")
	}
	seen := map[string]bool{}
	for index := range c.Routes {
		route := &c.Routes[index]
		if route.Name == "" {
			return invalid("route %d has no name", index)
		}
		if !contractIDPattern.MatchString(route.Handoff) {
			return invalid("route %s must name the handoff contract it serves", route.Name)
		}
		if route.Generation < 0 {
			return invalid("route %s has a negative generation", route.Name)
		}
		if len(route.Kinds) == 0 {
			return invalid("route %s admits no kind", route.Name)
		}
		for _, kind := range route.Kinds {
			if strings.HasPrefix(kind, ReturnReviewPrefix) {
				return invalid("route %s must not claim return reviews: the ingress answers them from its own state", route.Name)
			}
			if !slices.Contains([]string{KindWork, KindPlanningReview, KindFallback}, kind) {
				return invalid("route %s admits unknown kind %q", route.Name, kind)
			}
			key := route.Handoff + "/" + strconv.Itoa(route.Generation) + "/" + kind
			if seen[key] {
				return invalid("more than one route claims %s", key)
			}
			seen[key] = true
		}
		if len(route.Argv) == 0 {
			return invalid("route %s has no command", route.Name)
		}
		if route.TimeoutSeconds <= 0 {
			route.TimeoutSeconds = defaultTimeoutSeconds
		}
		if err := route.Outcome.normalize(route.Name); err != nil {
			return err
		}
	}
	return nil
}

// normalize fills the outcome defaults and refuses meanings Heartime does not
// accept. A route that declares no field cannot establish success, so every
// clean exit of it is uncertain and says so.
func (m *OutcomeMap) normalize(route string) error {
	defaults := []struct {
		target   *Outcome
		fallback Outcome
	}{
		{&m.WhenTrue, Verified},
		{&m.WhenFalse, Uncertain},
		{&m.OnRefusal, Contained},
		{&m.OnFailure, Failed},
		{&m.OnMissing, Uncertain},
	}
	for _, entry := range defaults {
		if *entry.target == "" {
			*entry.target = entry.fallback
		}
		if !validOutcome(*entry.target) {
			return fmt.Errorf("%w: route %s maps to %q, which Heartime does not accept", ErrInvalidConfig, route, *entry.target)
		}
	}
	return nil
}

// Route returns the relationship that owns one occurrence, if any. A
// generation of zero in the configuration means every generation of that
// handoff.
func (c Config) Route(evidence Evidence) (Route, bool) {
	for _, route := range c.Routes {
		if route.Handoff != evidence.Handoff.ID {
			continue
		}
		if route.Generation != 0 && route.Generation != evidence.Handoff.Generation {
			continue
		}
		if slices.Contains(route.Kinds, evidence.Kind) {
			return route, true
		}
	}
	return Route{}, false
}

// expand substitutes the activation's terms into a route's command. Only these
// placeholders exist; nothing from the delivery is ever interpreted as a
// command, and no value is passed through a shell.
func expand(argv []string, evidence Evidence, stateDir, deliveryPath string) []string {
	replacer := strings.NewReplacer(
		"{occurrence}", evidence.OccurrenceID,
		"{occurrenceHex}", short(evidence.OccurrenceID),
		"{stateDir}", stateDir,
		"{deliveryPath}", deliveryPath,
		"{contract}", evidence.Contract.ID,
		"{generation}", strconv.Itoa(evidence.Contract.Generation),
		"{obligation}", evidence.ObligationID,
		"{kind}", evidence.Kind,
		"{nominal}", evidence.Nominal,
	)
	expanded := make([]string, len(argv))
	for index, argument := range argv {
		expanded[index] = replacer.Replace(argument)
	}
	return expanded
}
