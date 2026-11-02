package app

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

type palette struct {
	dark      bool
	bg        ui.Color
	sidebar   ui.Color
	card      ui.Color
	text      ui.Color
	muted     ui.Color
	faint     ui.Color
	hairline  ui.Color
	hover     ui.Color
	press     ui.Color
	accent    ui.Color
	onAccent  ui.Color
	cardShade ui.Color
}

func paletteFor(t *ui.Theme) palette {
	if t.Dark {
		return palette{
			dark:      true,
			bg:        ui.Hex("#030303"),
			sidebar:   ui.Hex("#0f0f0f"),
			card:      ui.Hex("#1c1c1f"),
			text:      ui.Hex("#f1f1f1"),
			muted:     ui.Hex("#aaaaaa"),
			faint:     ui.RGBA(255, 255, 255, 0.25),
			hairline:  ui.RGBA(255, 255, 255, 0.08),
			hover:     ui.RGBA(255, 255, 255, 0.08),
			press:     ui.RGBA(255, 255, 255, 0.15),
			accent:    ui.Hex("#ff0000"),
			onAccent:  ui.Hex("#ffffff"),
			cardShade: ui.RGBA(0, 0, 0, 0.6),
		}
	}
	return palette{
		bg:        ui.Hex("#ffffff"),
		sidebar:   ui.Hex("#f8f8f8"),
		card:      ui.Hex("#f0f0f2"),
		text:      ui.Hex("#0f0f0f"),
		muted:     ui.Hex("#606060"),
		faint:     ui.RGBA(0, 0, 0, 0.25),
		hairline:  ui.RGBA(0, 0, 0, 0.08),
		hover:     ui.RGBA(0, 0, 0, 0.05),
		press:     ui.RGBA(0, 0, 0, 0.10),
		accent:    ui.Hex("#cc0000"),
		onAccent:  ui.Hex("#ffffff"),
		cardShade: ui.RGBA(0, 0, 0, 0.15),
	}
}

// formatTime formats duration as mm:ss or hh:mm:ss.
func formatTime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Seconds())
	m := s / 60
	s = s % 60
	if m >= 60 {
		h := m / 60
		m = m % 60
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
