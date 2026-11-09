package app

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/internal/ytmusic"
)

func (a *app) loadRelated() {
	cur := a.pl.Current()
	if cur == nil {
		return
	}
	if a.relatedVideoID == cur.ID && (a.relatedLoaded || a.relatedLoading) {
		return
	}

	a.relatedVideoID = cur.ID
	a.relatedSections = nil
	a.relatedLoading = true
	a.relatedLoaded = false
	a.npRelatedScroll = ui.ScrollState{}

	go func(videoID string) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		sections, err := ytmusic.DefaultClient.GetRelated(ctx, videoID)
		a.post(func() {
			if a.relatedVideoID != videoID {
				return
			}
			a.relatedLoading = false
			a.relatedLoaded = true
			if err == nil {
				a.relatedSections = sections
			}
		})
	}(cur.ID)
}
