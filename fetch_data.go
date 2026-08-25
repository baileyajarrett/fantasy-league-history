// fetch_data.go
//
// Walks a Sleeper fantasy football league's history and saves everything
// the website needs — users, rosters, weekly matchups, playoff brackets,
// transactions, draft picks, and player names — into a single JSON file
// (league-data.json) that league-history.html reads locally.
//
// Usage:
//
//	go run fetch_data.go -serve
//	go run fetch_data.go -league 1384692281021300736 -out league-data.json
//
// Or build a binary you can just double-click / re-run any time:
//
//	go build -o fetch_data fetch_data.go
//	./fetch_data -serve
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

const baseURL = "https://api.sleeper.app/v1"

// httpClient is shared across most requests. The player dictionary is a much
// larger download than anything else here, so it gets its own client with a
// longer timeout (see playersClient below).
var httpClient = &http.Client{Timeout: 15 * time.Second}
var playersClient = &http.Client{Timeout: 60 * time.Second}

// leagueMeta is the handful of fields we need to read ourselves in order to
// walk the season chain and know what else to fetch. Everything else in the
// league object gets passed through untouched via json.RawMessage in Bundle.
type leagueMeta struct {
	LeagueID         string `json:"league_id"`
	PreviousLeagueID string `json:"previous_league_id"`
	Season           string `json:"season"`
	DraftID          string `json:"draft_id"`
}

// Bundle holds one season's worth of raw data, in the shape the website
// expects. Everything is kept as json.RawMessage where possible so we don't
// need to model Sleeper's full schema in Go structs — we just forward the
// bytes we got from the API straight into the output file, and let the
// website's JavaScript do all the interpretation.
type Bundle struct {
	League         json.RawMessage   `json:"league"`
	Users          json.RawMessage   `json:"users"`
	Rosters        json.RawMessage   `json:"rosters"`
	WinnersBracket json.RawMessage   `json:"winnersBracket"`
	LosersBracket  json.RawMessage   `json:"losersBracket"`
	Weeks          []json.RawMessage `json:"weeks"`
	Transactions   []json.RawMessage `json:"transactions"`
	DraftPicks     json.RawMessage   `json:"draftPicks"`
}

// Output is the top-level structure written to league-data.json.
type Output struct {
	FetchedAt   time.Time                  `json:"fetchedAt"`
	LeagueID    string                     `json:"leagueId"`
	Bundles     []Bundle                   `json:"bundles"`
	PlayersByID map[string]json.RawMessage `json:"playersById"`
}

// fetchRaw does a GET request and returns the raw response body.
func fetchRaw(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response from %s failed: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}
	return body, nil
}

// fetchOptional is like fetchRaw, but returns an empty JSON array instead of
// an error. Several endpoints legitimately return nothing: winners_bracket
// on very old seasons, matchup/transaction weeks past the end of a season,
// leagues with no draft recorded, and so on.
func fetchOptional(url string) json.RawMessage {
	body, err := fetchRaw(url)
	if err != nil {
		return json.RawMessage("[]")
	}
	return json.RawMessage(body)
}

// walkChain follows previous_league_id backwards from startID and returns
// every season's raw league object, oldest season first.
func walkChain(startID string) ([]json.RawMessage, error) {
	var chain []json.RawMessage
	currentID := startID

	for i := 0; i < 15 && currentID != ""; i++ {
		url := fmt.Sprintf("%s/league/%s", baseURL, currentID)
		body, err := fetchRaw(url)
		if err != nil {
			return nil, fmt.Errorf("fetching league %s: %w", currentID, err)
		}

		var meta leagueMeta
		if err := json.Unmarshal(body, &meta); err != nil {
			return nil, fmt.Errorf("parsing league %s: %w", currentID, err)
		}

		chain = append(chain, json.RawMessage(body))

		if meta.PreviousLeagueID == "" || meta.PreviousLeagueID == "0" {
			break
		}
		currentID = meta.PreviousLeagueID
	}

	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

// loadBundle fetches everything needed for one season: users, rosters, the
// playoff bracket, up to 18 weeks of matchups, up to 18 weeks of
// transactions, and the season's draft picks (if any).
func loadBundle(leagueRaw json.RawMessage) (Bundle, error) {
	var meta leagueMeta
	if err := json.Unmarshal(leagueRaw, &meta); err != nil {
		return Bundle{}, err
	}

	usersBody, err := fetchRaw(fmt.Sprintf("%s/league/%s/users", baseURL, meta.LeagueID))
	if err != nil {
		return Bundle{}, err
	}
	rostersBody, err := fetchRaw(fmt.Sprintf("%s/league/%s/rosters", baseURL, meta.LeagueID))
	if err != nil {
		return Bundle{}, err
	}
	bracket := fetchOptional(fmt.Sprintf("%s/league/%s/winners_bracket", baseURL, meta.LeagueID))
	loserBracket := fetchOptional(fmt.Sprintf("%s/league/%s/losers_bracket", baseURL, meta.LeagueID))

	var draftPicks json.RawMessage = json.RawMessage("[]")
	if meta.DraftID != "" {
		draftPicks = fetchOptional(fmt.Sprintf("%s/draft/%s/picks", baseURL, meta.DraftID))
	}

	// Fetch all 18 possible weeks of matchups and transactions concurrently.
	// An NFL season never runs longer than 18 weeks, and asking for a week
	// or leg that doesn't exist yet just returns an empty array.
	type indexedResult struct {
		index int
		data  json.RawMessage
	}

	weeks := make([]json.RawMessage, 18)
	weekResults := make(chan indexedResult, 18)
	for w := 1; w <= 18; w++ {
		go func(week int) {
			url := fmt.Sprintf("%s/league/%s/matchups/%d", baseURL, meta.LeagueID, week)
			weekResults <- indexedResult{index: week - 1, data: fetchOptional(url)}
		}(w)
	}
	for i := 0; i < 18; i++ {
		r := <-weekResults
		weeks[r.index] = r.data
	}

	transactions := make([]json.RawMessage, 18)
	txResults := make(chan indexedResult, 18)
	for w := 1; w <= 18; w++ {
		go func(week int) {
			url := fmt.Sprintf("%s/league/%s/transactions/%d", baseURL, meta.LeagueID, week)
			txResults <- indexedResult{index: week - 1, data: fetchOptional(url)}
		}(w)
	}
	for i := 0; i < 18; i++ {
		r := <-txResults
		transactions[r.index] = r.data
	}

	return Bundle{
		League:         leagueRaw,
		Users:          json.RawMessage(usersBody),
		Rosters:        json.RawMessage(rostersBody),
		WinnersBracket: bracket,
		LosersBracket:  loserBracket,
		Weeks:          weeks,
		Transactions:   transactions,
		DraftPicks:     draftPicks,
	}, nil
}

// collectNeededPlayerIDs scans every bundle for player IDs that actually show
// up in this league's history (draft picks, and anyone who scored points in
// any week), so we only keep the slice of Sleeper's full player dictionary
// that this site will ever reference.
func collectNeededPlayerIDs(bundles []Bundle) map[string]bool {
	needed := make(map[string]bool)

	var picks []struct {
		PlayerID string `json:"player_id"`
	}
	var week []struct {
		PlayersPoints map[string]float64 `json:"players_points"`
	}

	for _, b := range bundles {
		picks = picks[:0]
		if err := json.Unmarshal(b.DraftPicks, &picks); err == nil {
			for _, p := range picks {
				if p.PlayerID != "" {
					needed[p.PlayerID] = true
				}
			}
		}
		for _, wk := range b.Weeks {
			week = week[:0]
			if err := json.Unmarshal(wk, &week); err != nil {
				continue
			}
			for _, entry := range week {
				for playerID := range entry.PlayersPoints {
					needed[playerID] = true
				}
			}
		}
	}
	return needed
}

// fetchPlayerDictionary downloads Sleeper's full NFL player list (a multi-
// megabyte one-time download) and returns just the trimmed fields we care
// about — name, position, team — for the player IDs we actually need.
func fetchPlayerDictionary(neededIDs map[string]bool) (map[string]json.RawMessage, error) {
	if len(neededIDs) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	resp, err := playersClient.Get(baseURL + "/players/nfl")
	if err != nil {
		return nil, fmt.Errorf("fetching player dictionary: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching player dictionary", resp.StatusCode)
	}

	var full map[string]struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Position  string `json:"position"`
		Team      string `json:"team"`
	}
	if err := json.Unmarshal(body, &full); err != nil {
		return nil, fmt.Errorf("parsing player dictionary: %w", err)
	}

	trimmed := make(map[string]json.RawMessage, len(neededIDs))
	for id := range neededIDs {
		p, ok := full[id]
		if !ok {
			continue
		}
		name := p.FirstName + " " + p.LastName
		encoded, err := json.Marshal(map[string]string{
			"name":     name,
			"position": p.Position,
			"team":     p.Team,
		})
		if err == nil {
			trimmed[id] = json.RawMessage(encoded)
		}
	}
	return trimmed, nil
}

// serve starts a plain static file server over the current directory so
// league-history.html can load league-data.json without hitting browser
// same-origin restrictions on file:// pages. It blocks forever (until
// Ctrl+C), like any dev server would.
func serve(port int) {
	addr := ":" + strconv.Itoa(port)
	url := fmt.Sprintf("http://localhost:%d/league-history.html", port)
	log.Printf("Serving this folder at http://localhost%s", addr)
	log.Printf("Open %s in your browser.", url)
	log.Println("Press Ctrl+C to stop.")
	if err := http.ListenAndServe(addr, http.FileServer(http.Dir("."))); err != nil {
		log.Fatalf("Couldn't start server: %v", err)
	}
}

func main() {
	leagueID := flag.String("league", "1384692281021300736", "Sleeper league ID (any season's ID works, history is walked automatically)")
	outPath := flag.String("out", "league-data.json", "where to write the resulting JSON file")
	doServe := flag.Bool("serve", false, "after fetching, also serve this folder over HTTP so you can open the site")
	port := flag.Int("port", 8000, "port to serve on, if -serve is set")
	flag.Parse()

	log.Printf("Walking season history starting from league %s...", *leagueID)
	chain, err := walkChain(*leagueID)
	if err != nil {
		log.Fatalf("Couldn't walk league history: %v", err)
	}
	log.Printf("Found %d season(s). Fetching details for each...", len(chain))

	bundles := make([]Bundle, 0, len(chain))
	for _, leagueRaw := range chain {
		var meta leagueMeta
		json.Unmarshal(leagueRaw, &meta)

		log.Printf("  season %s (league %s)...", meta.Season, meta.LeagueID)
		bundle, err := loadBundle(leagueRaw)
		if err != nil {
			log.Fatalf("Couldn't load season %s: %v", meta.Season, err)
		}
		bundles = append(bundles, bundle)
	}

	log.Println("Building player name lookup for drafted/rostered players...")
	neededIDs := collectNeededPlayerIDs(bundles)
	playersByID, err := fetchPlayerDictionary(neededIDs)
	if err != nil {
		// Not fatal — the site just won't be able to show player names.
		log.Printf("Warning: couldn't fetch player names (%v). Draft/roster pages will show player IDs instead.", err)
		playersByID = map[string]json.RawMessage{}
	} else {
		log.Printf("Resolved %d of %d referenced player IDs.", len(playersByID), len(neededIDs))
	}

	output := Output{
		FetchedAt:   time.Now(),
		LeagueID:    *leagueID,
		Bundles:     bundles,
		PlayersByID: playersByID,
	}

	file, err := os.Create(*outPath)
	if err != nil {
		log.Fatalf("Couldn't create %s: %v", *outPath, err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		log.Fatalf("Couldn't write JSON: %v", err)
	}

	log.Printf("Done! Wrote %s with %d season(s).", *outPath, len(bundles))

	if *doServe {
		serve(*port)
	} else {
		log.Println("Now serve this folder and open league-history.html — see README.md.")
	}
}
