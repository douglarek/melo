# Third-Party Notices

Melo is licensed under the GNU Affero General Public License, version 3
(AGPL-3.0; see [LICENSE](LICENSE)). Third-party components included in Melo's
source or binaries remain under their own licenses. This file lists those
components and where their license texts can be found.

## Linked components

| Component | Version | License | Copyright / notes |
| --- | --- | --- | --- |
| `github.com/egoist/mygo` | v0.7.4 | MIT | Copyright (c) 2026 MyGo contributors |
| `github.com/ebitengine/purego` | v0.11.1 | Apache-2.0 | See [licenses/Apache-2.0.txt](licenses/Apache-2.0.txt) |
| `github.com/tphakala/go-aac` | v0.7.1 | LGPL-2.1-or-later | See the LGPL section below; Go-derived portions are also under the Go BSD license |
| `github.com/tphakala/go-m4a` | v0.8.0 | MIT | Copyright (c) 2026 Tomi P. Hakala |
| `github.com/tphakala/simd` | v1.14.0 | MIT | Copyright (c) 2025 Tomi P. Hakala |
| `golang.org/x/image` | v0.46.0 | BSD-3-Clause | Copyright 2009 The Go Authors |
| `golang.org/x/sys` | v0.49.0 | BSD-3-Clause | Copyright 2009 The Go Authors |
| `golang.org/x/text` | v0.43.0 | BSD-3-Clause | Copyright 2009 The Go Authors |
| `github.com/go-text/typesetting` | v0.3.5 | BSD-3-Clause or Unlicense | Melo relies on the BSD-3-Clause option |

Full texts:

- [LICENSE](LICENSE) (AGPL-3.0 for Melo)
- [licenses/AGPL-3.0.txt](licenses/AGPL-3.0.txt)
- [licenses/MIT.txt](licenses/MIT.txt)
- [licenses/BSD-3-Clause.txt](licenses/BSD-3-Clause.txt)
- [licenses/Apache-2.0.txt](licenses/Apache-2.0.txt)
- [licenses/LGPL-2.1.txt](licenses/LGPL-2.1.txt)
- [licenses/LGPL-3.0.txt](licenses/LGPL-3.0.txt)
- [licenses/GPL-3.0.txt](licenses/GPL-3.0.txt) (referenced by LGPL-3.0)

## LGPL notice for go-aac

Melo statically links `github.com/tphakala/go-aac` v0.7.1, which is licensed
under the GNU Lesser General Public License, version 2.1 or later
(LGPL-2.1-or-later). Melo uses the LGPL-3.0 option for this component as part
of the AGPL-3.0 work. The complete corresponding source for this component is
available from:

- https://github.com/tphakala/go-aac (tag `v0.7.1`)
- `go mod download github.com/tphakala/go-aac@v0.7.1`

Melo's complete source, including the build scripts and dependency versions
needed to rebuild the application with a modified go-aac, is available at:

- https://github.com/douglarek/melo

For at least three years from the date a Melo binary is distributed, the
corresponding source for the LGPL component and the scripts needed to rebuild
that binary are available from the repository above. Recipients may modify the
LGPL component and rebuild the application from source. The LGPL-2.1 text is
included in [licenses/LGPL-2.1.txt](licenses/LGPL-2.1.txt); because the
component is licensed "or later", the LGPL-3.0 and GPL-3.0 texts are also
included, along with the full AGPL-3.0 text in
[licenses/AGPL-3.0.txt](licenses/AGPL-3.0.txt).

Melo's AGPL-3.0 source distribution also satisfies the source-availability
requirement for the LGPL component. The LGPL component itself remains under
its own license; this notice does not remove those obligations.

## External tools

Melo can invoke these optional system tools but does not distribute them:

- `ffmpeg` for HE-AAC audio transcoding and music-video decoding. The license
  of your locally installed ffmpeg build applies.
- `afconvert` on macOS as an HE-AAC fallback.
- `notify-send` on Linux for desktop notifications.

## Design reference

Melo's interface was designed with reference to
[mygo-cloudmusic](https://github.com/egoist/mygo-cloudmusic), which is licensed
under AGPL-3.0. It is not a Go dependency of Melo. Melo is also AGPL-3.0; any
code reused from that project remains AGPL-3.0 and its attribution is retained.
