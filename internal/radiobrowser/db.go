package radiobrowser

import (
	"database/sql"
	"embed"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"retuner/internal/config"

	_ "modernc.org/sqlite" // pure Go SQLite, no cgo
)

// The SQL scripts are the unchanged src/sql/*.sql resources of the Pascal build.
//
//go:embed sql/*.sql
var sqlFS embed.FS

func script(name string) string {
	b, err := sqlFS.ReadFile("sql/" + name + ".sql")
	if err != nil {
		panic(err)
	}
	return string(b)
}

const (
	dbFile    = "rb.db"
	dbTmpFile = "rb-tmp.db"
	chunkLen  = 100000
)

var dbCategoryField = [...]string{"tag", "country", "language", "tc", "name"}

// DB is the SQLite directory cache (RBCacheType catDB, catMemDB, catPermMemDB).
// catDB serves from the file rb.db; catMemDB builds in memory each start;
// catPermMemDB keeps rb.db as the saved copy and serves from a memory copy.
type DB struct {
	c       *Client
	typ     string
	dir     string
	ttl     time.Duration
	db      *sql.DB
	created time.Time
	views   map[string]string
}

func openDB(dsn string) (*sql.DB, error) {
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1) // temp tables and the memory database live on one connection
	return d, d.Ping()
}

var memSeq atomic.Int64

// memDSN gives every in-memory database its own name, so a rebuild never mixes with the one in use.
func memDSN() string {
	return "file:rbmem" + strconv.FormatInt(memSeq.Add(1), 10) + "?mode=memory&cache=shared"
}

func (b *DB) fileFresh(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return b.ttl <= 0 || time.Since(fi.ModTime()) < b.ttl
}

// PrepareDB opens (or builds) the directory database. It blocks while building,
// as the Pascal build does at start-up. An error means the cache type cannot be
// served; the caller must not fall back to live API traffic silently.
func (c *Client) PrepareDB(dir string, cacheType string, ttlHours int) (*DB, error) {
	b := &DB{c: c, typ: cacheType, dir: dir, ttl: time.Duration(ttlHours) * time.Hour, views: map[string]string{}}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create database directory %s: %w", dir, err)
	}
	log.Printf("Checking local database")
	file := filepath.Join(dir, dbFile)
	switch cacheType {
	case "catDB":
		if b.fileFresh(file) {
			d, err := openDB(file)
			if err != nil {
				return nil, err
			}
			b.db = d
			fi, _ := os.Stat(file)
			b.created = fi.ModTime()
			log.Printf("Local database is ready")
			return b, nil
		}
	case "catPermMemDB":
		if b.fileFresh(file) {
			if d, err := restoreToMemory(file); err == nil {
				b.db = d
				fi, _ := os.Stat(file)
				b.created = fi.ModTime()
				log.Printf("Local database is ready")
				return b, nil
			} else {
				log.Printf("Can not restore database (%v)", err)
			}
		}
	}
	if err := b.build(); err != nil {
		return nil, err
	}
	log.Printf("Local database is ready")
	return b, nil
}

func restoreToMemory(file string) (*sql.DB, error) {
	d, err := openDB(memDSN())
	if err != nil {
		return nil, err
	}
	if _, err = d.Exec("ATTACH DATABASE '" + strings.ReplaceAll(file, "'", "''") + "' AS saved"); err != nil {
		return nil, err
	}
	// Copy schema and data table by table (no backup API in database/sql).
	rows, err := d.Query("SELECT name, sql FROM saved.sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return nil, err
	}
	type tbl struct{ name, ddl string }
	var tbls []tbl
	for rows.Next() {
		var t tbl
		if err = rows.Scan(&t.name, &t.ddl); err != nil {
			return nil, err
		}
		tbls = append(tbls, t)
	}
	rows.Close()
	for _, t := range tbls {
		if _, err = d.Exec(t.ddl); err != nil {
			return nil, err
		}
		if _, err = d.Exec("INSERT INTO main." + quote(t.name) + " SELECT * FROM saved." + quote(t.name)); err != nil {
			return nil, err
		}
	}
	for _, kind := range []string{"index", "view"} {
		r2, e := d.Query("SELECT sql FROM saved.sqlite_master WHERE type='" + kind + "' AND sql IS NOT NULL")
		if e != nil {
			return nil, e
		}
		var stmts []string
		for r2.Next() {
			var s string
			_ = r2.Scan(&s)
			stmts = append(stmts, s)
		}
		r2.Close()
		for _, s := range stmts {
			if _, err = d.Exec(s); err != nil {
				return nil, err
			}
		}
	}
	if _, err = d.Exec("DETACH DATABASE saved"); err != nil {
		return nil, err
	}
	return d, nil
}

func quote(n string) string { return `"` + strings.ReplaceAll(n, `"`, `""`) + `"` }

func (b *DB) build() error {
	var d *sql.DB
	var err error
	tmp := filepath.Join(b.dir, dbTmpFile)
	switch b.typ {
	case "catDB":
		_ = os.Remove(tmp)
		d, err = openDB(tmp)
	default:
		d, err = openDB(memDSN())
		if err == nil {
			// a rebuild must start from an empty memory database
			for _, t := range []string{"stationstags", "stations", "tags", "codecs", "languages", "countries"} {
				_, _ = d.Exec("DROP TABLE IF EXISTS " + t)
			}
		}
	}
	if err != nil {
		return fmt.Errorf("database connection: %w", err)
	}
	log.Printf("Preparing local database. This may take a while..")
	step := func(what string, f func() error) error {
		if e := f(); e != nil {
			d.Close()
			return fmt.Errorf("error %s: %w", what, e)
		}
		return nil
	}
	plain := func(name string) func() error { return func() error { _, e := d.Exec(script(name)); return e } }
	withJSON := func(name, path string) func() error {
		return func() error { return b.execWithJSON(d, script(name), path) }
	}
	steps := []struct {
		what string
		f    func() error
	}{
		{"creating tables", plain("RBDB_SQL_CREATE_TABLES")},
		{"filling countries table", withJSON("RBDB_SQL_INSERT_COUNTRIES", "countries")},
		{"filling languages table", withJSON("RBDB_SQL_INSERT_LANGUAGES", "languages")},
		{"filling codecs table", withJSON("RBDB_SQL_INSERT_CODECS", "codecs")},
		{"filling tags table", withJSON("RBDB_SQL_INSERT_TAGS", "tags")},
		{"creating indexes", plain("RBDB_SQL_CREATE_IDX")},
		{"filling stations table", withJSON("RBDB_SQL_INSERT_STATIONS", "stations")},
		{"cleaning tables", plain("RBDB_SQL_FIX_DATA")},
		{"rebuilding indexes", plain("RBDB_SQL_CREATE_REIDX")},
	}
	for _, s := range steps {
		if err := step(s.what, s.f); err != nil {
			return err
		}
	}
	file := filepath.Join(b.dir, dbFile)
	switch b.typ {
	case "catDB":
		d.Close()
		_ = os.Remove(file)
		if err := os.Rename(tmp, file); err != nil {
			return fmt.Errorf("cannot move %s to %s: %w", dbTmpFile, dbFile, err)
		}
		if d, err = openDB(file); err != nil {
			return err
		}
	case "catPermMemDB":
		_ = os.Remove(file)
		if _, err := d.Exec("VACUUM INTO '" + strings.ReplaceAll(file, "'", "''") + "'"); err != nil {
			log.Printf("Can not backup database (%v)", err)
		}
	}
	old := b.db
	b.db, b.created, b.views = d, time.Now(), map[string]string{}
	if old != nil && old != d {
		old.Close()
	}
	return nil
}

// execWithJSON downloads one directory list and runs the script steps over it:
// steps carrying the JSON marker run once per chunk of the list, the others once.
func (b *DB) execWithJSON(d *sql.DB, steps, path string) error {
	raw := b.c.get(addBulk(path))
	js := strings.TrimSpace(string(raw))
	if len(js) < 2 || js[0] != '[' {
		return fmt.Errorf("no valid answer for %s", path)
	}
	if js == "[]" {
		// get() reports a failed request as an empty list. A directory without
		// countries or stations is a failed download, never a usable database.
		if path == "countries" || path == "stations" {
			return fmt.Errorf("no data received for %s", path)
		}
		return nil
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(js, "["), "]")
	var chunks []string
	for len(inner) > 0 {
		cut := len(inner)
		if len(inner) > chunkLen {
			if i := strings.Index(inner[chunkLen:], "},{"); i >= 0 {
				cut = chunkLen + i + 1
			}
		}
		chunks = append(chunks, inner[:cut])
		inner = strings.TrimPrefix(inner[cut:], ",")
	}
	for _, st := range strings.Split(steps, "--Next Step--") {
		if !strings.Contains(st, "@@@JSONCONTENTS") {
			if _, err := d.Exec(st); err != nil {
				return err
			}
			continue
		}
		for _, ch := range chunks {
			lit := "'[" + strings.ReplaceAll(ch, "'", "''") + "]'"
			if _, err := d.Exec(strings.ReplaceAll(st, "@@@JSONCONTENTS", lit)); err != nil {
				return err
			}
		}
	}
	return nil
}

// safeSQLIdent guards the few values that are spliced into SQL text (the view
// name, the ORDER BY column) because SQLite cannot bind identifiers. Values
// that are data (category, search text, station id) are bound as parameters.
var safeSQLIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func isSafeSQLIdent(s string) bool { return safeSQLIdent.MatchString(s) }

func viewName(mac string, a *config.AVR) string {
	return "avr" + mac + "_" + strconv.FormatUint(uint64(a.FileCRC), 10)
}

func deleteSection(lines []string, marker string) []string {
	for {
		start := -1
		for i, l := range lines {
			if strings.TrimRight(l, "\r") == "--Begin "+marker {
				start = i
				break
			}
		}
		if start < 0 {
			return lines
		}
		end := start
		for end < len(lines) && strings.TrimRight(lines[end], "\r") != "--End "+marker {
			end++
		}
		if end < len(lines) {
			end++
		}
		lines = append(lines[:start], lines[end:]...)
	}
}

// ensureView (re)creates this receiver's filtered view and drops views left by
// an earlier version of its ini file (DBRBCheckAVRView).
func (b *DB) ensureView(mac string, a *config.AVR) (string, error) {
	name := viewName(mac, a)
	if !isSafeSQLIdent(name) {
		return "", fmt.Errorf("invalid view name")
	}
	if cn, ok := b.views[name]; ok {
		return cn, nil
	}
	rows, err := b.db.Query(script("RBDB_SQL_LIST_VIEWS"))
	if err != nil {
		return "", err
	}
	var old []string
	for rows.Next() {
		var v string
		_ = rows.Scan(&v)
		if p := strings.Split(v, "_"); len(p) == 2 && p[0] == "avr"+mac && p[1] != strconv.FormatUint(uint64(a.FileCRC), 10) {
			old = append(old, v)
		}
	}
	rows.Close()
	for _, v := range old {
		if _, e := b.db.Exec(strings.ReplaceAll(script("RBDB_SQL_DROP_VIEW_AVR"), "@@@AVR", v)); e != nil {
			log.Printf("delete view error: %s", v)
		}
	}
	lines := strings.Split(script("RBDB_SQL_CREATE_VIEW_AVR"), "\n")
	text := func() string { return strings.Join(lines, "\n") }
	setVar := func(marker string, vals []string) {
		v := config.StripChars(strings.Join(vals, ";"), filterStripChars)
		lines = strings.Split(strings.ReplaceAll(strings.ReplaceAll(text(), "@@@"+marker, v), "*", "%"), "\n")
	}
	setVar("AVR", []string{name})
	sec := func(marker string, vals []string) {
		if len(vals) == 0 {
			lines = deleteSection(lines, marker)
		} else {
			setVar(marker, vals)
		}
	}
	sec("AllowedTags", a.AllowedTags)
	sec("NotAllowedTags", a.NotAllowedTags)
	sec("AllowedCountries", a.AllowedCountries)
	sec("AllowedLanguages", a.AllowedLanguages)
	sec("AllowedCodecs", a.AllowedCodecs)
	sec("NotAllowedCodecs", a.NotAllowedCodecs)
	sec("NotAllowedInName", a.NotAllowedInName)
	sec("NotAllowedInURL", a.NotAllowedInURL)
	if a.BitrateMax == 0 {
		lines = deleteSection(lines, "BitrateMax")
	} else {
		setVar("BitrateMax", []string{strconv.Itoa(a.BitrateMax)})
	}
	if _, err := b.db.Exec(text()); err != nil {
		return "", fmt.Errorf("create view: %w", err)
	}
	cn, err := b.confirmedViewName(name)
	if err != nil {
		return "", err
	}
	b.views[name] = cn
	return cn, nil
}

// confirmedViewName resolves a candidate view name against the database
// catalogue and returns the name as SQLite stores it. SELECT statements splice
// the result into their text because SQLite cannot bind identifiers, so that
// string must originate from the database itself, never from request input.
func (b *DB) confirmedViewName(name string) (string, error) {
	if !isSafeSQLIdent(name) {
		return "", fmt.Errorf("invalid view name")
	}
	rows, err := b.db.Query(script("RBDB_SQL_LIST_VIEWS"))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return "", err
		}
		if v == name {
			return v, nil
		}
	}
	return "", fmt.Errorf("radio-browser view missing after creation")
}

const filterStripChars = "!\"'():@&=+$,/?%#[]-.~\n\r\t"

func (b *DB) Categories(k Kind, a *config.AVR, mac string, start, howMany int) ([]Category, int) {
	v, err := b.ensureView(mac, a)
	if err != nil || !isSafeSQLIdent(v) {
		log.Printf("Radio-browser database: %v", err)
		return nil, 0
	}
	q := script("RBDB_SQL_SELECT_CATEGORY")
	q = strings.ReplaceAll(q, "@@@Category", dbCategoryField[k])
	q = strings.ReplaceAll(q, "@@@AVR", v)
	q = strings.ReplaceAll(q, "@@@Start", strconv.Itoa(start))
	q = strings.ReplaceAll(q, "@@@Offset", strconv.Itoa(howMany))
	rows, err := b.db.Query(q)
	if err != nil {
		log.Printf("Radio-browser database query error (%v)", err)
		return nil, 0
	}
	defer rows.Close()
	var out []Category
	total := 0
	for rows.Next() {
		var c Category
		if rows.Scan(&total, &c.Name, &c.Count) == nil {
			out = append(out, c)
		}
	}
	return out, total
}

func (b *DB) Stations(k Kind, name string, a *config.AVR, mac string, start, howMany int) ([]Station, int) {
	v, err := b.ensureView(mac, a)
	if err != nil || !isSafeSQLIdent(v) {
		log.Printf("Radio-browser database: %v", err)
		return nil, 0
	}
	q := script("RBDB_SQL_SELECT_STATIONS")
	q = strings.ReplaceAll(q, "@@@AVR", v)
	dir := "ASC"
	if a.Reverse {
		dir = "DESC"
	}
	clean := config.StripChars(httpDecodeName(name), filterStripChars)
	var cat, order string
	var args []any
	switch k {
	case Genre, Country, Language:
		cat, order = dbCategoryField[k]+" = ?", a.Order
		args = append(args, clean)
	case Popular:
		cat, order, dir = "tc=1", "votes", "DESC"
	case Search:
		cat, order = "name LIKE ?", a.Order
		args = append(args, "%"+clean+"%")
	}
	if !isSafeSQLIdent(order) {
		order = "name"
	}
	q = strings.ReplaceAll(q, "@@@Category", cat)
	q = strings.ReplaceAll(q, "@@@Order", order)
	q = strings.ReplaceAll(q, "@@@DirOrder", dir)
	q = strings.ReplaceAll(q, "@@@Start", strconv.Itoa(start))
	q = strings.ReplaceAll(q, "@@@Offset", strconv.Itoa(howMany))
	rows, err := b.db.Query(q, args...)
	if err != nil {
		log.Printf("Radio-browser database query error (%v)", err)
		return nil, 0
	}
	defer rows.Close()
	var out []Station
	total := 0
	for rows.Next() {
		var s Station
		var votes, bitrate sql.NullString
		var codec sql.NullString
		if rows.Scan(&total, &s.ID, &s.Name, &s.URL, &s.Homepage, &s.Icon, &votes, &bitrate, &s.Country, &s.Language, &codec, &s.Tags) == nil {
			s.Votes, s.Bitrate, s.Codec = votes.String, bitrate.String, codec.String
			out = append(out, s)
		}
	}
	return out, total
}

func (b *DB) ByID(id12 string, a *config.AVR, mac string) (Station, bool) {
	v, err := b.ensureView(mac, a)
	if err != nil || !isSafeSQLIdent(v) {
		return Station{}, false
	}
	id := config.StripChars(id12, filterStripChars)
	if len(id) < 8 {
		return Station{}, false
	}
	id = id[:8] + "-" + id[8:]
	q := strings.ReplaceAll(script("RBDB_SQL_SELECT_STATION"), "@@@AVR", v)
	q = strings.ReplaceAll(q, "@@@ID", "?")
	var s Station
	var votes, bitrate, codec sql.NullString
	if err := b.db.QueryRow(q, id+"-%").Scan(&s.ID, &s.Name, &s.URL, &s.Homepage, &s.Icon, &votes, &bitrate, &s.Country, &s.Language, &codec, &s.Tags); err != nil {
		return Station{}, false
	}
	s.Votes, s.Bitrate, s.Codec = votes.String, bitrate.String, codec.String
	return s, true
}

// RootCounts returns the directory sizes for the top menu: tags, countries,
// languages and the stations that pass the receiver's filters.
func (b *DB) RootCounts(a *config.AVR, mac string) (genre, country, language, popular int, ok bool) {
	v, err := b.ensureView(mac, a)
	if err != nil {
		return
	}
	q := strings.ReplaceAll(script("RBDB_SQL_SELECT_ROOT"), "@@@AVR", v)
	if err := b.db.QueryRow(q).Scan(&language, &country, &genre, &popular); err != nil {
		return
	}
	return genre, country, language, popular, true
}

// Refresh rebuilds the database (RBCacheTTL > 0): into a new database first, then
// swapped in, so requests keep being served meanwhile.
func (b *DB) Refresh() error { return b.build() }

func httpDecodeName(s string) string {
	if u, err := urlUnescape(s); err == nil {
		return u
	}
	return s
}

func urlUnescape(s string) (string, error) { return url.PathUnescape(s) }
