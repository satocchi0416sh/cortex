package sync

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/satocchi0416sh/cortex/internal/config"
	"github.com/satocchi0416sh/cortex/internal/markdown"
	"github.com/satocchi0416sh/cortex/internal/notion"
	"github.com/satocchi0416sh/cortex/internal/scanner"
	"github.com/satocchi0416sh/cortex/internal/state"
)

type Result struct {
	Created int
	Updated int
	Skipped int
	Failed  int
	Total   int
	Plan    []PlanItem
}

type PlanItem struct {
	Action     string
	ExternalID string
	Project    string
	RelPath    string
	Filename   string
}

type Runner struct {
	cfg    *config.Config
	logger *slog.Logger
	client *notion.Client
	dryRun bool
}

func New(cfg *config.Config, logger *slog.Logger, dryRun bool) *Runner {
	r := &Runner{cfg: cfg, logger: logger, dryRun: dryRun}
	if !dryRun {
		r.client = notion.NewClient(notion.Options{
			Token:      cfg.NotionToken,
			DatabaseID: cfg.NotionDatabaseID,
			RPS:        cfg.RPS,
			Logger:     logger,
		})
	}
	return r
}

func (r *Runner) Run(ctx context.Context) (*Result, error) {
	st, err := state.Load(r.cfg.StateFile)
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}

	files, err := scanner.Scan(r.cfg.ScanRoot, r.cfg.GlobPattern)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	r.logger.Info("scan complete", "files", len(files), "root", r.cfg.ScanRoot)

	res := &Result{Total: len(files)}

	for _, f := range files {
		entry, exists := st.Get(f.ExternalID)
		action := decideAction(entry, exists, f.ContentHash)

		res.Plan = append(res.Plan, PlanItem{
			Action:     action,
			ExternalID: f.ExternalID,
			Project:    f.Project,
			RelPath:    f.RelPath,
			Filename:   f.Filename,
		})

		if r.dryRun {
			r.logger.Info("plan", "action", action, "project", f.Project, "rel", f.RelPath)
			switch action {
			case "create":
				res.Created++
			case "update":
				res.Updated++
			case "skip":
				res.Skipped++
			}
			continue
		}

		switch action {
		case "skip":
			res.Skipped++
			continue
		case "create":
			pageID, err := r.create(ctx, f)
			if err != nil {
				r.logger.Error("create failed", "file", f.AbsPath, "err", err)
				res.Failed++
				continue
			}
			st.Set(f.ExternalID, toEntry(f, pageID))
			res.Created++
		case "update":
			pageID := entry.PageID
			if pageID == "" {
				found, err := r.client.FindPageByExternalID(ctx, f.ExternalID)
				if err != nil {
					r.logger.Error("lookup failed", "file", f.AbsPath, "err", err)
					res.Failed++
					continue
				}
				pageID = found
			}
			if pageID == "" {
				newID, err := r.create(ctx, f)
				if err != nil {
					r.logger.Error("create-on-update failed", "file", f.AbsPath, "err", err)
					res.Failed++
					continue
				}
				st.Set(f.ExternalID, toEntry(f, newID))
				res.Created++
				continue
			}
			if err := r.update(ctx, pageID, f); err != nil {
				r.logger.Error("update failed", "file", f.AbsPath, "err", err)
				res.Failed++
				continue
			}
			st.Set(f.ExternalID, toEntry(f, pageID))
			res.Updated++
		}
	}

	if !r.dryRun {
		if err := st.Save(); err != nil {
			return res, fmt.Errorf("save state: %w", err)
		}
	}
	return res, nil
}

func decideAction(entry state.FileEntry, exists bool, hash string) string {
	if !exists {
		return "create"
	}
	if entry.ContentHash == hash && entry.PageID != "" {
		return "skip"
	}
	return "update"
}

func toEntry(f scanner.File, pageID string) state.FileEntry {
	return state.FileEntry{
		AbsPath:     f.AbsPath,
		Project:     f.Project,
		RelPath:     f.RelPath,
		Filename:    f.Filename,
		ContentHash: f.ContentHash,
		PageID:      pageID,
		LastSynced:  time.Now().UTC(),
	}
}

func (r *Runner) create(ctx context.Context, f scanner.File) (string, error) {
	blocks := markdown.Convert(f.Body)
	props := r.props(f)
	return r.client.CreatePage(ctx, props, blocks)
}

func (r *Runner) update(ctx context.Context, pageID string, f scanner.File) error {
	blocks := markdown.Convert(f.Body)
	if err := r.client.ReplaceChildren(ctx, pageID, blocks); err != nil {
		return err
	}
	return r.client.UpdatePageProperties(ctx, pageID, r.props(f))
}

func (r *Runner) props(f scanner.File) notion.Properties {
	return notion.Properties{
		Title:       scanner.TitleFromFilename(f.Filename),
		ExternalID:  f.ExternalID,
		Project:     f.Project,
		FilePath:    f.RelPath,
		ContentHash: f.ContentHash,
		LastSynced:  time.Now().UTC(),
	}
}
