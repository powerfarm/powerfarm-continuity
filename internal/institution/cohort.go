package institution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CohortSchema identifies the manifest this package freezes for one occurrence.
const CohortSchema = "powerfarm.census.cohort/v0"

// ErrCohortAuthority means the recognized topology could not be resolved under
// an institutional machine authority. It is never a reason to fall back to a
// remembered cohort: a census that reconciles against a cohort nobody
// re-established is asserting recognition it does not have.
var ErrCohortAuthority = errors.New("the expected cohort could not be resolved under an institutional authority")

// CohortAuthority resolves the currently recognized topology of one place.
//
// It is an interface because the authority is the institution's, not this
// package's: Continuity asks, the Registry decides, and the answer is frozen
// for exactly one occurrence. No implementation may return a remembered or
// operator-supplied cohort.
type CohortAuthority interface {
	// Resolve returns the manifest for one occurrence and the exact bytes the
	// authority answered with, so the resolution itself stays reviewable.
	Resolve(ctx context.Context, place, occurrence string) (CohortManifest, []byte, error)
}

// CohortPlace is the place a census occurrence inventories, as the Registry
// recognizes it.
type CohortPlace struct {
	ID      string `json:"id"`
	Path    string `json:"path,omitempty"`
	Machine string `json:"machine,omitempty"`
}

// CohortMember is one entity the Registry currently recognizes at that place.
type CohortMember struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Path string `json:"path,omitempty"`
}

// CohortProvenance names who resolved a cohort and under what authority, so a
// reconciliation can never be read as Registry-current when it was not.
type CohortProvenance struct {
	Source    string `json:"source"`
	Endpoint  string `json:"endpoint"`
	Mechanism string `json:"mechanism"`
	Grant     string `json:"grant"`
	Identity  string `json:"identity,omitempty"`
}

// CohortManifest is the expected cohort of exactly one census occurrence.
//
// It is resolved before the sweep starts, frozen as immutable bytes, and
// referenced by digest for the rest of the occurrence. It is a reading of the
// Registry at an instant, not a copy of the Registry and not operational state:
// nothing writes it back, and a later census resolves its own.
type CohortManifest struct {
	Schema      string           `json:"schema"`
	Occurrence  string           `json:"occurrence"`
	ResolvedAt  string           `json:"resolvedAt"`
	Authority   CohortProvenance `json:"authority"`
	Place       CohortPlace      `json:"place"`
	Members     []CohortMember   `json:"members"`
	Limitations []string         `json:"limitations"`
}

// Validate refuses a manifest that could not be reconciled against honestly.
func (m CohortManifest) Validate(place, occurrence string) error {
	invalid := func(reason string) error { return fmt.Errorf("%w: %s", ErrCohortAuthority, reason) }
	if m.Schema != CohortSchema {
		return invalid("the manifest does not declare " + CohortSchema)
	}
	if m.Occurrence != occurrence {
		return invalid("the manifest was not resolved for this occurrence")
	}
	if m.Place.ID != place {
		return invalid("the manifest describes " + m.Place.ID + ", not " + place)
	}
	if _, err := time.Parse(time.RFC3339, m.ResolvedAt); err != nil {
		return invalid("resolvedAt must be RFC 3339")
	}
	if m.Authority.Source == "" || m.Authority.Mechanism == "" || m.Authority.Grant == "" {
		return invalid("the manifest does not say under what authority it was resolved")
	}
	seen := map[string]bool{}
	for _, member := range m.Members {
		if member.ID == "" || member.Kind == "" {
			return invalid("every member must name an identity and a kind")
		}
		if seen[member.ID] {
			return invalid("the manifest names " + member.ID + " twice")
		}
		seen[member.ID] = true
	}
	return nil
}

// Expected projects the manifest onto what reconciliation compares against.
func (m CohortManifest) Expected() []Expected {
	expected := make([]Expected, 0, len(m.Members))
	for _, member := range m.Members {
		expected = append(expected, Expected{ID: member.ID, Place: m.Place.ID})
	}
	return expected
}

// RegistryCohort resolves the cohort from the Powerfarm Registry with a
// least-privilege machine credential.
//
// The credential is a Registry service credential bound to a machine identity,
// and the Registry decides for itself what that identity may read. Nothing here
// holds a human session, and nothing here can read anything the grant does not
// cover: the scope is the Registry's to enforce, not this client's to promise.
type RegistryCohort struct {
	// Endpoint is the authority's read operation, for example
	// https://<project>.supabase.co/rest/v1/rpc/powerfarm_place_cohort.
	Endpoint string
	// APIKey admits the request to the Registry's HTTP surface. It is not an
	// authority: it identifies no one and grants nothing.
	APIKey string
	// Token is the machine service credential. The Registry resolves the calling
	// identity from it and never returns it.
	Token string
	// Grant is the action the credential's identity must hold, recorded in the
	// manifest so a reader knows what authority produced it.
	Grant string
	HTTP  *http.Client
}

// registryAnswer is what the Registry's cohort read returns.
type registryAnswer struct {
	ResolvedAt  string         `json:"resolvedAt"`
	Identity    string         `json:"identity"`
	Place       CohortPlace    `json:"place"`
	Members     []CohortMember `json:"members"`
	Limitations []string       `json:"limitations"`
}

// Resolve asks the Registry what it currently recognizes at one place.
func (r RegistryCohort) Resolve(ctx context.Context, place, occurrence string) (CohortManifest, []byte, error) {
	// Say which half is missing: a human reading the containment needs to know
	// whether the read does not exist or the credential for it does not.
	switch {
	case r.Endpoint == "" && r.Token == "":
		return CohortManifest{}, nil, fmt.Errorf("%w: no Registry cohort read and no machine service credential are configured", ErrCohortAuthority)
	case r.Endpoint == "":
		return CohortManifest{}, nil, fmt.Errorf("%w: no Registry cohort read is configured", ErrCohortAuthority)
	case r.Token == "":
		return CohortManifest{}, nil, fmt.Errorf("%w: no machine service credential is configured for %s, so this census holds no institutional authority to read the cohort", ErrCohortAuthority, r.Endpoint)
	}
	body, err := json.Marshal(map[string]string{"p_token": r.Token, "p_place": place})
	if err != nil {
		return CohortManifest{}, nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.Endpoint, bytes.NewReader(body))
	if err != nil {
		return CohortManifest{}, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if r.APIKey != "" {
		request.Header.Set("apikey", r.APIKey)
	}
	client := r.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return CohortManifest{}, nil, fmt.Errorf("%w: %v", ErrCohortAuthority, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return CohortManifest{}, raw, fmt.Errorf("%w: %v", ErrCohortAuthority, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return CohortManifest{}, raw, fmt.Errorf("%w: the Registry answered HTTP %d", ErrCohortAuthority, response.StatusCode)
	}
	var answer registryAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		return CohortManifest{}, raw, fmt.Errorf("%w: the Registry's answer is not a cohort: %v", ErrCohortAuthority, err)
	}
	resolved := answer.ResolvedAt
	if resolved == "" {
		resolved = time.Now().UTC().Format(time.RFC3339)
	}
	manifest := CohortManifest{
		Schema: CohortSchema, Occurrence: occurrence, ResolvedAt: resolved,
		Authority: CohortProvenance{
			Source: "powerfarm-registry", Endpoint: r.Endpoint,
			Mechanism: "registry-service-credential", Grant: r.Grant, Identity: answer.Identity,
		},
		Place: answer.Place, Members: answer.Members, Limitations: answer.Limitations,
	}
	if manifest.Members == nil {
		manifest.Members = []CohortMember{}
	}
	if manifest.Limitations == nil {
		manifest.Limitations = []string{}
	}
	if err := manifest.Validate(place, occurrence); err != nil {
		return CohortManifest{}, raw, err
	}
	return manifest, raw, nil
}

// BoundaryHumanOnlyAuthority is the Direction boundary reached when the
// decision required is one only a human holding institutional authority can
// make. Issuing a Registry grant to a machine identity is such a decision: no
// amount of technical recovery can substitute for it, and no route may work
// around it.
const BoundaryHumanOnlyAuthority = "human-only-authority"
