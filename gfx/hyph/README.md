# Hyphenation patterns

TeX hyphenation patterns, embedded in `gfx` and loaded on first use by
`gfx.HyphenatorFor`. Every file comes from the hyph-utf8 project
(<https://github.com/hyphenation/tex-hyphen>, the
`hyph-utf8/tex/generic/hyph-utf8/patterns/tex` directory) with its
contents unchanged; only the `hyph-` prefix of the upstream file name is
dropped, so `hyph-en-gb.tex` is `en-gb.tex` here. Each file keeps its own
comment header, which carries the title, the copyright, the licence and
the hyphenmins the parser reads.

To add a language, copy its file from hyph-utf8 with the header intact,
drop the `hyph-` prefix, add the tag to `hyphFiles` in `gfx/hyphen.go`,
and list it below with its licence. A file whose licence is unclear does
not go in.

| File | Tag | Language | Upstream file | Copyright | Licence |
|---|---|---|---|---|---|
| `da.tex` | `da` | Danish | `hyph-da.tex` | 1994 Frank Jensen | LPPL 1.3 or later, or MIT |
| `de-1996.tex` | `de` | German, reformed spelling | `hyph-de-1996.tex` | 2013-2024 the wortliste authors | MIT |
| `en-gb.tex` | `en-gb` | English, British spelling | `hyph-en-gb.tex` | 1992-2016 Dominik Wujastyk, Graham Toal | MIT |
| `en-us.tex` | `en`, `en-us` | English, American spelling | `hyph-en-us.tex` | 1990-2005 Gerard D.C. Kuiken | Free use with the notice preserved |
| `es.tex` | `es` | Spanish | `hyph-es.tex` | 1993-2019 Javier Bezos, CervanTeX | MIT/X11 |
| `fi.tex` | `fi` | Finnish | `hyph-fi.tex` | 1986-1989 Kauko Saarinen | "Patterns may be freely distributed" |
| `fr.tex` | `fr` | French | `hyph-fr.tex` | 1994-2016 Daniel Flipo, Bernard Gaulle, Arthur Reutenauer | MIT |
| `it.tex` | `it` | Italian | `hyph-it.tex` | 2008-2011 Claudio Beccari | LPPL 1.3 or later, or MIT |
| `nb.tex` | `nb` | Norwegian Bokmål | `hyph-nb.tex` | 2004-2007 Rune Kleveland, Ole Michael Selberg, Karl Ove Hufthammer | Free use with the notice preserved |
| `nl.tex` | `nl` | Dutch | `hyph-nl.tex` | 1996 Piet Tutelaers | MIT |
| `no.tex` | `no` | Norwegian, both forms | `hyph-no.tex` | 2004-2005 Rune Kleveland, Ole Michael Selberg | Free use with the notice preserved |
| `pl.tex` | `pl` | Polish | `hyph-pl.tex` | 1987-1995 Hanna Kołodziejska, Bogusław Jackowski, Marek Ryćko | MIT, or public domain on Knuth's terms |
| `pt.tex` | `pt` | Portuguese | `hyph-pt.tex` | 1987-2024 Pedro J. de Rezende, J. Joao Dias Almeida, Leonardo Araujo, Aline Benevides | BSD 3-clause |
| `ru.tex` | `ru` | Russian | `hyph-ru.tex` | 1999-2003 Alexander I. Lebedev | LPPL 1.2 or later |
| `sv.tex` | `sv` | Swedish | `hyph-sv.tex` | 1994 Jan Michael Rynning | LPPL 1.2 or later |

`nb.tex` holds only exceptions and inputs `no.tex` for its patterns, the
way it does in TeX; the loader reads both.

Finnish is the one file whose licence is a sentence rather than a named
licence: its header says only that the patterns may be freely
distributed, which is what shipping them does. Every other file names a
licence or repeats the free-use notice in full.
