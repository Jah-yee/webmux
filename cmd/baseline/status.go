package main

import (
	"fmt"
	"time"

	"github.com/rainchan/webmux/internal/attentionkind"
	"github.com/rainchan/webmux/internal/session"
)

func (f *fixture) status() metric {
	type scenario struct {
		name, script string
		needsHuman   bool
	}
	event := func(name string) string {
		return fmt.Sprintf(`printf '\033]777;notify;warp://cli-agent;{"v":1,"agent":"omp","event":"%s"}\a'; while :; do printf 'still working\n'; sleep 0.2; done`, name)
	}
	cases := []scenario{
		{"continuous_output", `while :; do printf 'working\n'; sleep 0.2; done`, false},
		{"question_prompt", `printf 'Continue? '; read -r answer`, true},
		{"permission_prompt", `printf '[y/n] '; read -r answer`, true},
		{"unmarked_input", `read -r answer`, true},
		{"agent_question", event("question_asked"), true},
		{"agent_permission", event("permission_request"), true},
		{"unattended_sleep", `printf 'background timer\n'; sleep 60`, false},
		{"completed", `printf 'done\n'; exit 0`, false},
	}
	var ids []string
	defer func() {
		for _, id := range ids {
			f.dispose(id)
		}
	}()
	for _, v := range cases {
		ids = append(ids, f.create(v.script))
	}
	time.Sleep(3500 * time.Millisecond)
	observed := make([]string, len(cases))
	m := measure("needs_human_synthetic_corpus", "case", len(cases), func(i int) float64 {
		v := f.info(ids[i])
		predicted := v.Status == session.StatusWaiting
		observed[i] = fmt.Sprintf("%s: needs_human=%t, status=%s, predicted=%t", cases[i].name, cases[i].needsHuman, v.Status, predicted)
		if cases[i].name == "agent_question" {
			check(v.Attention != nil && v.Attention.Kind == attentionkind.Question, "agent question signal missing")
		}
		if cases[i].name == "agent_permission" {
			check(v.Attention != nil && v.Attention.Kind == attentionkind.Permission, "agent permission signal missing")
		}
		check(predicted == cases[i].needsHuman, "%s", observed[i])
		return 1
	})
	for i := range m.Samples {
		m.Samples[i].Case = observed[i]
	}
	m.CorrectnessGate = false
	return m
}
