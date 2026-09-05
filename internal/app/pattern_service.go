package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"claude-code-hist-viewer/internal/adapter/cluster"
	"claude-code-hist-viewer/internal/domain"
)

type PatternKind string

const (
	PatternSkills  PatternKind = "skills"
	PatternScripts PatternKind = "scripts"
	PatternAll     PatternKind = "all"
)

const (
	minClusterSessions = 3
	medoidSampleCount  = 5
	minNGramSessions   = 3
	ngramMinN          = 2
	ngramMaxN          = 4
	ngramMaxSeqGap     = 3
)

var autoKCandidates = []int{8, 12, 16, 24, 32}

type PatternOptions struct {
	Kind        PatternKind
	K           int
	Top         int
	Vendor      domain.Vendor
	ProjectPath string
	Since       time.Time
	NoLLM       bool
}

type SkillCandidate struct {
	Name           string   `json:"name"`
	Intent         string   `json:"intent"`
	TriggerPhrases []string `json:"trigger_phrases"`
	Outline        []string `json:"outline"`
	Confidence     float64  `json:"confidence"`
	Sessions       int      `json:"sessions"`
	Projects       int      `json:"projects"`
	Samples        []string `json:"samples"`
}

type ScriptCandidate struct {
	Name      string   `json:"name"`
	Purpose   string   `json:"purpose"`
	Script    string   `json:"script"`
	Params    []string `json:"params"`
	RiskNotes string   `json:"risk_notes"`
	Sessions  int      `json:"sessions"`
	NGram     []string `json:"ngram"`
	Samples   []string `json:"samples"`
}

type PatternReport struct {
	Skills  []SkillCandidate  `json:"skills"`
	Scripts []ScriptCandidate `json:"scripts"`
}

type PatternService struct {
	repo domain.EmbeddingRepository
	chat domain.ChatModel
}

func NewPatternService(repo domain.EmbeddingRepository, chat domain.ChatModel) *PatternService {
	return &PatternService{repo: repo, chat: chat}
}

func (s *PatternService) Run(ctx context.Context, embedModel string, opts PatternOptions) (PatternReport, error) {
	if err := s.repo.InitEmbeddings(ctx); err != nil {
		return PatternReport{}, fmt.Errorf("init embeddings: %w", err)
	}
	top := opts.Top
	if top <= 0 {
		top = 15
	}
	filter := domain.EmbedFilter{Vendor: opts.Vendor, ProjectPath: opts.ProjectPath, Since: opts.Since}

	var report PatternReport
	if opts.Kind == PatternSkills || opts.Kind == PatternAll || opts.Kind == "" {
		skills, err := s.mineSkills(ctx, embedModel, filter, opts, top)
		if err != nil {
			return report, err
		}
		report.Skills = skills
	}
	if opts.Kind == PatternScripts || opts.Kind == PatternAll || opts.Kind == "" {
		scripts, err := s.mineScripts(ctx, embedModel, filter, opts, top)
		if err != nil {
			return report, err
		}
		report.Scripts = scripts
	}
	return report, nil
}

func (s *PatternService) mineSkills(ctx context.Context, model string, filter domain.EmbedFilter, opts PatternOptions, top int) ([]SkillCandidate, error) {
	skillFilter := filter
	skillFilter.Roles = []domain.Role{domain.RoleUser}
	skillFilter.Kinds = []domain.BlockKind{domain.KindText}

	items, err := s.repo.EmbeddingsWithContext(ctx, model, skillFilter)
	if err != nil {
		return nil, err
	}
	if len(items) < minClusterSessions {
		return nil, nil
	}

	res := clusterEmbeddings(items, opts.K)
	groups := groupByCluster(items, res)
	ranked := rankClusters(groups)
	if len(ranked) > top {
		ranked = ranked[:top]
	}

	out := make([]SkillCandidate, 0, len(ranked))
	for _, g := range ranked {
		cand := SkillCandidate{
			Sessions: distinctSessions(g),
			Projects: distinctProjects(g),
			Samples:  medoidSamples(g, res, medoidSampleCount),
		}
		if !opts.NoLLM && s.chat != nil {
			if labeled, err := s.labelSkill(ctx, cand.Samples); err == nil {
				cand.Name = labeled.Name
				cand.Intent = labeled.Intent
				cand.TriggerPhrases = labeled.TriggerPhrases
				cand.Outline = labeled.Outline
				cand.Confidence = labeled.Confidence
			}
		}
		if cand.Name == "" {
			cand.Name = fmt.Sprintf("cluster-%d", len(out)+1)
			if len(cand.Samples) > 0 {
				cand.Intent = firstLine(cand.Samples[0])
			}
		}
		out = append(out, cand)
	}
	return out, nil
}

func (s *PatternService) mineScripts(ctx context.Context, model string, filter domain.EmbedFilter, opts PatternOptions, top int) ([]ScriptCandidate, error) {
	home, _ := os.UserHomeDir()

	bashUnits, err := s.repo.BashUnits(ctx, filter)
	if err != nil {
		return nil, err
	}
	grams := mineNGrams(bashUnits, home)

	var candidates []ScriptCandidate
	for _, g := range grams {
		sessions := len(g.sessions)
		if sessions < minNGramSessions {
			continue
		}
		candidates = append(candidates, ScriptCandidate{
			NGram:    g.gram,
			Samples:  g.gram,
			Sessions: sessions,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Sessions*len(candidates[i].NGram) > candidates[j].Sessions*len(candidates[j].NGram)
	})

	scriptFilter := filter
	scriptFilter.Kinds = []domain.BlockKind{domain.KindToolUse}
	if items, err := s.repo.EmbeddingsWithContext(ctx, model, scriptFilter); err == nil && len(items) >= minClusterSessions {
		res := clusterEmbeddings(items, opts.K)
		groups := groupByCluster(items, res)
		for _, g := range rankClusters(groups) {
			samples := commandSamples(medoidSamples(g, res, medoidSampleCount))
			if len(samples) == 0 || isTrivialCommand(samples[0]) {
				continue
			}
			candidates = append(candidates, ScriptCandidate{
				Samples:  samples,
				Sessions: distinctSessions(g),
			})
		}
	}

	if len(candidates) > top {
		candidates = candidates[:top]
	}
	for i := range candidates {
		if !opts.NoLLM && s.chat != nil {
			if labeled, err := s.labelScript(ctx, candidates[i].Samples); err == nil {
				candidates[i].Name = labeled.Name
				candidates[i].Purpose = labeled.Purpose
				candidates[i].Script = labeled.Script
				candidates[i].Params = labeled.Params
				candidates[i].RiskNotes = labeled.RiskNotes
			}
		}
		if candidates[i].Name == "" {
			candidates[i].Name = fmt.Sprintf("script-candidate-%d", i+1)
			if len(candidates[i].Samples) > 0 {
				candidates[i].Purpose = firstLine(candidates[i].Samples[0])
			}
		}
	}
	return candidates, nil
}

// --- clustering helpers, shared by skill and script mining ---

type clusterGroup struct {
	index   int
	items   []domain.EmbeddingContext
	vectors [][]float32
}

func clusterEmbeddings(items []domain.EmbeddingContext, k int) cluster.Result {
	points := make([][]float32, len(items))
	for i, it := range items {
		points[i] = it.Vector
	}
	if k > 0 {
		return cluster.Run(points, k, 42, 50)
	}
	return cluster.PickK(points, autoKCandidates, 2000, 42, 50)
}

func groupByCluster(items []domain.EmbeddingContext, res cluster.Result) []clusterGroup {
	byIdx := make(map[int]*clusterGroup)
	for i, it := range items {
		c := res.Assignments[i]
		g, ok := byIdx[c]
		if !ok {
			g = &clusterGroup{index: c}
			byIdx[c] = g
		}
		g.items = append(g.items, it)
		g.vectors = append(g.vectors, it.Vector)
	}
	groups := make([]clusterGroup, 0, len(byIdx))
	for _, g := range byIdx {
		groups = append(groups, *g)
	}
	return groups
}

func distinctSessions(g clusterGroup) int {
	seen := make(map[string]bool)
	for _, it := range g.items {
		seen[it.SessionID] = true
	}
	return len(seen)
}

func distinctProjects(g clusterGroup) int {
	seen := make(map[string]bool)
	for _, it := range g.items {
		if it.ProjectPath != "" {
			seen[it.ProjectPath] = true
		}
	}
	return len(seen)
}

// rankClusters drops clusters with fewer than minClusterSessions distinct
// sessions and orders the rest by distinct_sessions * spread_across_projects.
func rankClusters(groups []clusterGroup) []clusterGroup {
	var kept []clusterGroup
	for _, g := range groups {
		if distinctSessions(g) >= minClusterSessions {
			kept = append(kept, g)
		}
	}
	rank := func(g clusterGroup) int {
		projects := distinctProjects(g)
		if projects == 0 {
			projects = 1
		}
		return distinctSessions(g) * projects
	}
	sort.Slice(kept, func(i, j int) bool { return rank(kept[i]) > rank(kept[j]) })
	return kept
}

// medoidSamples returns up to n sample texts closest to the cluster centroid.
func medoidSamples(g clusterGroup, res cluster.Result, n int) []string {
	type scored struct {
		text string
		dist float64
	}
	centroid := res.Centroids[g.index]
	items := make([]scored, len(g.vectors))
	for i, v := range g.vectors {
		items[i] = scored{text: g.items[i].Text, dist: sqDist(v, centroid)}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].dist < items[j].dist })
	if len(items) > n {
		items = items[:n]
	}
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.text
	}
	return out
}

func sqDist(a, b []float32) float64 {
	var sum float64
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		sum += d * d
	}
	return sum
}

// --- Bash command n-gram mining (exact match, no embeddings needed) ---

var numberOrHexRe = regexp.MustCompile(`\b[0-9a-fA-F]{6,}\b|\b[0-9]+\b`)

func normalizeCommand(cmd, home string) string {
	if home != "" {
		cmd = strings.ReplaceAll(cmd, home, "~")
	}
	return strings.TrimSpace(numberOrHexRe.ReplaceAllString(cmd, "<x>"))
}

type ngramHit struct {
	gram     []string
	sessions map[string]bool
}

// mineNGrams builds per-session n-grams (n=2..4) of consecutive normalized
// Bash commands whose block-sequence gap stays within ngramMaxSeqGap, then
// counts how many distinct sessions each n-gram appears in.
func mineNGrams(units []domain.EmbedUnit, home string) map[string]*ngramHit {
	bySession := make(map[string][]domain.EmbedUnit)
	for _, u := range units {
		bySession[u.SessionID] = append(bySession[u.SessionID], u)
	}

	grams := make(map[string]*ngramHit)
	for sessionID, seq := range bySession {
		sort.Slice(seq, func(i, j int) bool { return seq[i].Seq < seq[j].Seq })
		cmds := make([]string, len(seq))
		for i, u := range seq {
			cmds[i] = normalizeCommand(bashCommandFromText(u.Text), home)
		}
		for n := ngramMinN; n <= ngramMaxN; n++ {
			for i := 0; i+n <= len(cmds); i++ {
				if seq[i+n-1].Seq-seq[i].Seq > (n-1)*(ngramMaxSeqGap+1) {
					continue
				}
				tokens := cmds[i : i+n]
				if tokens[0] == "" || tokens[n-1] == "" {
					continue
				}
				key := strings.Join(tokens, " && ")
				g, ok := grams[key]
				if !ok {
					g = &ngramHit{gram: append([]string(nil), tokens...), sessions: make(map[string]bool)}
					grams[key] = g
				}
				g.sessions[sessionID] = true
			}
		}
	}
	return grams
}

// commandSamples extracts the "command" field from raw Bash tool_use message
// text (as stored: `Bash {"command":"..."}`) for each sample, dropping any
// that fail to parse.
func commandSamples(rawTexts []string) []string {
	out := make([]string, 0, len(rawTexts))
	for _, raw := range rawTexts {
		if cmd := strings.TrimSpace(bashCommandFromText(raw)); cmd != "" {
			out = append(out, cmd)
		}
	}
	return out
}

var trivialCommands = map[string]bool{"ls": true, "cat": true, "pwd": true, "git status": true}

func isTrivialCommand(cmd string) bool {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return true
	}
	if trivialCommands[strings.Join(fields, " ")] {
		return true
	}
	return len(fields) == 1 && trivialCommands[fields[0]]
}

// --- LLM labeling ---

type skillLabel struct {
	Name           string   `json:"name"`
	Intent         string   `json:"intent"`
	TriggerPhrases []string `json:"trigger_phrases"`
	Outline        []string `json:"outline"`
	Confidence     float64  `json:"confidence"`
}

func (s *PatternService) labelSkill(ctx context.Context, samples []string) (skillLabel, error) {
	system := "You analyze repeated user requests to a coding agent and propose a reusable skill. " +
		`Respond with a single JSON object only, no prose, matching: {"name":"kebab-case-slug","intent":"one line","trigger_phrases":["..."],"outline":["step 1","step 2"],"confidence":0.0}`
	user := "Sample requests from one cluster:\n" + strings.Join(numberLines(samples), "\n")
	raw, err := s.chat.Chat(ctx, system, user)
	if err != nil {
		return skillLabel{}, err
	}
	var label skillLabel
	if err := json.Unmarshal([]byte(extractJSON(raw)), &label); err != nil {
		return skillLabel{}, err
	}
	return label, nil
}

type scriptLabel struct {
	Name      string   `json:"name"`
	Purpose   string   `json:"purpose"`
	Script    string   `json:"script"`
	Params    []string `json:"params"`
	RiskNotes string   `json:"risk_notes"`
}

func (s *PatternService) labelScript(ctx context.Context, samples []string) (scriptLabel, error) {
	system := "You analyze repeated shell command sequences from a coding agent's history and draft a reusable script. " +
		`Respond with a single JSON object only, no prose, matching: {"name":"kebab-case-slug","purpose":"one line","script":"#!/usr/bin/env bash\n...","params":["..."],"risk_notes":"..."}`
	user := "Repeated command sequence samples:\n" + strings.Join(samples, "\n---\n")
	raw, err := s.chat.Chat(ctx, system, user)
	if err != nil {
		return scriptLabel{}, err
	}
	var label scriptLabel
	if err := json.Unmarshal([]byte(extractJSON(raw)), &label); err != nil {
		return scriptLabel{}, err
	}
	return label, nil
}

func extractJSON(s string) string {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end < 0 || end < start {
		return s
	}
	return s[start : end+1]
}

func numberLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = fmt.Sprintf("%d. %s", i+1, firstLine(l))
	}
	return out
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
