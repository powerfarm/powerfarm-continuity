package journal

/*
#cgo linux LDFLAGS: -lsqlite3
#cgo darwin LDFLAGS: -lsqlite3
#include <sqlite3.h>
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"powerfarm.dev/continuity/v2/internal/effects"
)

type Journal struct {
	mu sync.Mutex
	db *C.sqlite3
}

type Record struct {
	EffectID          string        `json:"effectId"`
	BundleDigest      string        `json:"bundleDigest"`
	StepName          string        `json:"stepName"`
	Capability        string        `json:"capability"`
	CapabilityVersion string        `json:"capabilityVersion"`
	Class             effects.Class `json:"class"`
	InputDigest       string        `json:"inputDigest"`
	State             effects.State `json:"state"`
	UpdatedAt         string        `json:"updatedAt"`
	LastError         string        `json:"lastError,omitempty"`
}

type EventRecord struct {
	ID         int64         `json:"id"`
	EffectID   string        `json:"effectId"`
	FromState  effects.State `json:"fromState"`
	Event      effects.Event `json:"event"`
	ToState    effects.State `json:"toState"`
	OccurredAt string        `json:"occurredAt"`
	Details    any           `json:"details,omitempty"`
}

func Open(path string) (*Journal, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var db *C.sqlite3
	flags := C.int(C.SQLITE_OPEN_READWRITE | C.SQLITE_OPEN_CREATE | C.SQLITE_OPEN_FULLMUTEX)
	if rc := C.sqlite3_open_v2(cpath, &db, flags, nil); rc != C.SQLITE_OK {
		msg := "sqlite open failed"
		if db != nil {
			msg = C.GoString(C.sqlite3_errmsg(db))
			C.sqlite3_close(db)
		}
		return nil, fmt.Errorf("%s", msg)
	}
	j := &Journal{db: db}
	if err := j.init(); err != nil {
		_ = j.Close()
		return nil, err
	}
	return j, nil
}

func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.db == nil {
		return nil
	}
	rc := C.sqlite3_close(j.db)
	if rc != C.SQLITE_OK {
		return fmt.Errorf("sqlite close: %s", C.GoString(C.sqlite3_errmsg(j.db)))
	}
	j.db = nil
	return nil
}

func (j *Journal) init() error {
	schema := `
PRAGMA journal_mode=WAL;
PRAGMA synchronous=FULL;
CREATE TABLE IF NOT EXISTS effects (
    effect_id TEXT PRIMARY KEY,
    bundle_digest TEXT NOT NULL,
    step_name TEXT NOT NULL,
    capability TEXT NOT NULL,
    capability_version TEXT NOT NULL,
    class TEXT NOT NULL,
    input_digest TEXT NOT NULL,
    state TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_error TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS effect_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    effect_id TEXT NOT NULL,
    from_state TEXT NOT NULL,
    event TEXT NOT NULL,
    to_state TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    details_json TEXT NOT NULL DEFAULT '{}',
    FOREIGN KEY(effect_id) REFERENCES effects(effect_id)
);
CREATE INDEX IF NOT EXISTS effect_events_effect_id_idx ON effect_events(effect_id, id);
`
	return j.exec(schema)
}

func (j *Journal) Create(r Record) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if r.EffectID == "" || r.BundleDigest == "" || r.StepName == "" || r.Capability == "" || r.InputDigest == "" {
		return fmt.Errorf("effect record missing required identity fields")
	}
	if r.Class == "" {
		return fmt.Errorf("effect class is required")
	}
	if r.State == "" {
		r.State = effects.Planned
	}
	if r.State != effects.Planned {
		return fmt.Errorf("new effects must start planned, got %q", r.State)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	stmt, err := j.prepare(`INSERT INTO effects(effect_id,bundle_digest,step_name,capability,capability_version,class,input_digest,state,updated_at,last_error) VALUES(?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer C.sqlite3_finalize(stmt)
	values := []string{r.EffectID, r.BundleDigest, r.StepName, r.Capability, r.CapabilityVersion, string(r.Class), r.InputDigest, string(r.State), now, r.LastError}
	for i, v := range values {
		if err := bindText(stmt, i+1, v); err != nil {
			return err
		}
	}
	if rc := C.sqlite3_step(stmt); rc != C.SQLITE_DONE {
		return j.err("insert effect")
	}
	return nil
}

func (j *Journal) Get(effectID string) (Record, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.getLocked(effectID)
}

func (j *Journal) Transition(effectID string, event effects.Event, details any) (Record, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	current, err := j.getLocked(effectID)
	if err != nil {
		return Record{}, err
	}
	next, err := effects.Next(current.State, event)
	if err != nil {
		return Record{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	detailBytes, err := json.Marshal(details)
	if err != nil {
		return Record{}, err
	}
	if len(detailBytes) == 0 || string(detailBytes) == "null" {
		detailBytes = []byte("{}")
	}

	if err := j.exec("BEGIN IMMEDIATE"); err != nil {
		return Record{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = j.exec("ROLLBACK")
		}
	}()

	update, err := j.prepare(`UPDATE effects SET state=?, updated_at=? WHERE effect_id=? AND state=?`)
	if err != nil {
		return Record{}, err
	}
	for i, v := range []string{string(next), now, effectID, string(current.State)} {
		if err := bindText(update, i+1, v); err != nil {
			C.sqlite3_finalize(update)
			return Record{}, err
		}
	}
	rc := C.sqlite3_step(update)
	C.sqlite3_finalize(update)
	if rc != C.SQLITE_DONE {
		return Record{}, j.err("update effect state")
	}
	if C.sqlite3_changes(j.db) != 1 {
		return Record{}, fmt.Errorf("effect %q state changed concurrently", effectID)
	}

	ins, err := j.prepare(`INSERT INTO effect_events(effect_id,from_state,event,to_state,occurred_at,details_json) VALUES(?,?,?,?,?,?)`)
	if err != nil {
		return Record{}, err
	}
	vals := []string{effectID, string(current.State), string(event), string(next), now, string(detailBytes)}
	for i, v := range vals {
		if err := bindText(ins, i+1, v); err != nil {
			C.sqlite3_finalize(ins)
			return Record{}, err
		}
	}
	rc = C.sqlite3_step(ins)
	C.sqlite3_finalize(ins)
	if rc != C.SQLITE_DONE {
		return Record{}, j.err("insert effect event")
	}

	if err := j.exec("COMMIT"); err != nil {
		return Record{}, err
	}
	committed = true
	current.State = next
	current.UpdatedAt = now
	return current, nil
}

func (j *Journal) Events(effectID string) ([]EventRecord, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	stmt, err := j.prepare(`SELECT id,effect_id,from_state,event,to_state,occurred_at,details_json FROM effect_events WHERE effect_id=? ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer C.sqlite3_finalize(stmt)
	if err := bindText(stmt, 1, effectID); err != nil {
		return nil, err
	}
	var out []EventRecord
	for {
		rc := C.sqlite3_step(stmt)
		if rc == C.SQLITE_DONE {
			break
		}
		if rc != C.SQLITE_ROW {
			return nil, j.err("query effect events")
		}
		raw := columnText(stmt, 6)
		var details any
		_ = json.Unmarshal([]byte(raw), &details)
		out = append(out, EventRecord{
			ID: int64(C.sqlite3_column_int64(stmt, 0)), EffectID: columnText(stmt, 1), FromState: effects.State(columnText(stmt, 2)), Event: effects.Event(columnText(stmt, 3)), ToState: effects.State(columnText(stmt, 4)), OccurredAt: columnText(stmt, 5), Details: details,
		})
	}
	return out, nil
}

func (j *Journal) getLocked(effectID string) (Record, error) {
	stmt, err := j.prepare(`SELECT effect_id,bundle_digest,step_name,capability,capability_version,class,input_digest,state,updated_at,last_error FROM effects WHERE effect_id=?`)
	if err != nil {
		return Record{}, err
	}
	defer C.sqlite3_finalize(stmt)
	if err := bindText(stmt, 1, effectID); err != nil {
		return Record{}, err
	}
	rc := C.sqlite3_step(stmt)
	if rc == C.SQLITE_DONE {
		return Record{}, fmt.Errorf("effect %q not found", effectID)
	}
	if rc != C.SQLITE_ROW {
		return Record{}, j.err("query effect")
	}
	return Record{
		EffectID: columnText(stmt, 0), BundleDigest: columnText(stmt, 1), StepName: columnText(stmt, 2), Capability: columnText(stmt, 3), CapabilityVersion: columnText(stmt, 4), Class: effects.Class(columnText(stmt, 5)), InputDigest: columnText(stmt, 6), State: effects.State(columnText(stmt, 7)), UpdatedAt: columnText(stmt, 8), LastError: columnText(stmt, 9),
	}, nil
}

func (j *Journal) prepare(sql string) (*C.sqlite3_stmt, error) {
	csql := C.CString(sql)
	defer C.free(unsafe.Pointer(csql))
	var stmt *C.sqlite3_stmt
	if rc := C.sqlite3_prepare_v2(j.db, csql, -1, &stmt, nil); rc != C.SQLITE_OK {
		return nil, j.err("prepare")
	}
	return stmt, nil
}

func (j *Journal) exec(sql string) error {
	csql := C.CString(sql)
	defer C.free(unsafe.Pointer(csql))
	var errmsg *C.char
	rc := C.sqlite3_exec(j.db, csql, nil, nil, &errmsg)
	if rc != C.SQLITE_OK {
		msg := "sqlite exec failed"
		if errmsg != nil {
			msg = C.GoString(errmsg)
			C.sqlite3_free(unsafe.Pointer(errmsg))
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func (j *Journal) err(op string) error {
	return fmt.Errorf("%s: %s", op, C.GoString(C.sqlite3_errmsg(j.db)))
}

func bindText(stmt *C.sqlite3_stmt, idx int, value string) error {
	c := C.CString(value)
	defer C.free(unsafe.Pointer(c))
	if rc := C.sqlite3_bind_text(stmt, C.int(idx), c, C.int(len(value)), (*[0]byte)(C.SQLITE_TRANSIENT)); rc != C.SQLITE_OK {
		return fmt.Errorf("sqlite bind %d failed", idx)
	}
	return nil
}

func columnText(stmt *C.sqlite3_stmt, idx int) string {
	p := C.sqlite3_column_text(stmt, C.int(idx))
	if p == nil {
		return ""
	}
	return C.GoString((*C.char)(unsafe.Pointer(p)))
}
