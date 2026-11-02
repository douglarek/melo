# Tray icons

The app embeds two PNGs:

- `tray-template.png` is the macOS menu bar icon. It follows the app icon's
  rounded-square silhouette, with the ring and play triangle cut out. It is
  black on transparent; `mygo.NewTray` tints it for light and dark menu bars.
  The image is 36×36 pixels, which is 18 points at 2×.
- `tray-color.png` is the Linux and Windows notification-area icon. It is
  64×64 pixels.

The SVGs are the editable sources. After changing an SVG, regenerate its PNG
from this directory:

```bash
ffmpeg -y -i tray-template.svg tray-template.png
ffmpeg -y -i tray-color.svg tray-color.png
```

Any SVG rasterizer works; keep the transparent background for the template
and preserve the PNG dimensions. Then rebuild the app. No Go code changes are
needed.
