package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/okitaAsuka/asuka-nas/internal/nas"
	"io"
	"os"
)

// rulesCLI 只操作本地规则库；与 daemon 共用锁，绝不连接上游。
func rulesCLI(args []string) error {
	if len(args) < 1 {
		return errors.New("rule_action_required")
	}
	action := args[0]
	fs := flag.NewFlagSet("rules", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	config := fs.String("config", "config.json", "")
	file := fs.String("file", "", "")
	id := fs.String("id", "", "")
	revision := fs.Int("revision", 1, "")
	title := fs.String("title", "", "")
	source := fs.String("source", "", "")
	job := fs.String("job", "", "")
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 {
		return errors.New("rule_arguments_invalid")
	}
	if action == "parse" {
		p, e := nas.ParseTitle(*title)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(p)
	}
	f, e := os.Open(*config)
	if e != nil {
		return errors.New("configuration_unavailable")
	}
	cfg, e := nas.LoadConfig(f)
	f.Close()
	if e != nil {
		return e
	}
	lock, e := nas.AcquireInstance(cfg.Database)
	if e != nil {
		return e
	}
	defer lock.Close()
	s, e := nas.OpenStore(cfg.Database)
	if e != nil {
		return e
	}
	defer s.Close()
	ctx := context.Background()
	switch action {
	case "add":
		f, e := os.Open(*file)
		if e != nil {
			return errors.New("rule_file_unavailable")
		}
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, 65537))
		if e != nil || len(b) > 65536 {
			return errors.New("rule_file_invalid")
		}
		var r nas.NamingRule
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if dec.Decode(&r) != nil || dec.Decode(new(any)) != io.EOF {
			return errors.New("rule_file_invalid")
		}
		if e = s.AddRule(ctx, r); e != nil {
			return e
		}
	case "enable", "disable":
		if e = s.SetRule(ctx, *id, *revision, action == "enable"); e != nil {
			return e
		}
	case "preview":
		r, e := s.Rule(ctx, *id, *revision)
		if e != nil {
			return e
		}
		d, e := r.Resolve(*title, *source)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(d)
	case "replan":
		if e = s.Replan(ctx, *job); e != nil {
			return e
		}
	default:
		return errors.New("rule_action_invalid")
	}
	fmt.Println("rule_action_completed")
	return nil
}
