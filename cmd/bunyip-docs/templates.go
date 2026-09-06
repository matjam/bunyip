package main

import "strings"

// page is the data every template sees.
type page struct {
	Site     *Site
	URL      string
	Title    string
	Markdown string // the same page as Markdown, relative to the site root
	Guide    *Guide
	Program  *Program
	Programs bool // this is the list of example programs
	Package  *Package
}

// Root is the relative path back to the site root from this page.
func (p page) Root() string { return strings.Repeat("../", strings.Count(p.URL, "/")) }

// Active reports whether a sidebar link points at this page.
func (p page) Active(url string) bool { return p.URL == url }

const layoutTmpl = `{{define "layout"}}<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · Bunyip</title>
{{if .Markdown}}<link rel="alternate" type="text/markdown" href="{{.Root}}{{.Markdown}}" title="{{.Title}} as Markdown">
{{end}}<link rel="stylesheet" href="{{.Root}}site.css">
<script defer src="{{.Root}}site.js" data-root="{{.Root}}"></script>
</head>
<body>
<header class="top">
<button class="menu" aria-label="Menu">☰</button>
<a class="brand" href="{{.Root}}index.html">Bunyip</a>
<span class="tagline">a game engine in Go</span>
<div class="search"><input id="search" type="search" placeholder="Search symbols…" autocomplete="off"><div id="results" class="results" hidden></div></div>
<a class="gh" href="` + repo + `">GitHub</a>
</header>
<div class="shell">
<nav class="side">
{{range .Site.GuideGroups}}<section><h4>{{.Title}} guides</h4><ul>
{{range .Guides}}<li><a href="{{$.Root}}guides/{{.Slug}}.html"{{if $.Active (printf "guides/%s.html" .Slug)}} class="active"{{end}}>{{.Title}}</a></li>
{{end}}</ul></section>
{{end}}{{range .Site.Groups}}<section><h4>{{.Title}}</h4><ul>
{{range .Packages}}<li><a href="{{$.Root}}{{.URL}}"{{if $.Active .URL}} class="active"{{end}}>{{short .Rel}}</a></li>
{{end}}</ul></section>
{{end}}{{if .Site.Programs}}<section><h4>Example programs</h4><ul>
<li><a href="{{.Root}}examples/index.html"{{if .Active "examples/index.html"}} class="active"{{end}}>All examples</a></li>
{{range .Site.Programs}}<li><a href="{{$.Root}}{{.URL}}"{{if $.Active .URL}} class="active"{{end}}>{{.Title}}</a></li>
{{end}}</ul></section>
{{end}}</nav>
<main class="content">
{{template "body" .}}
</main>
</div>
</body>
</html>{{end}}`

const indexTmpl = `{{define "index"}}{{template "layout" .}}{{end}}
{{define "body"}}{{if .Package}}{{template "packageBody" .}}{{else if .Guide}}{{template "guideBody" .}}{{else if .Program}}{{template "programBody" .}}{{else if .Programs}}{{template "programIndexBody" .}}{{else}}{{template "indexBody" .}}{{end}}{{end}}
{{define "indexBody"}}
<div class="hero">
<h1>Bunyip</h1>
<p class="lead">A game engine in Go for real-time and turn-based games: roguelikes, 4X, arcade, and anything that wants 2D sprites and 3D models on the same screen. Vulkan underneath, no cgo, native window and audio layers, and every subsystem a game needs from the first frame to the shipped build.</p>
<p class="actions"><a class="button" href="guides/getting-started.html">Get started</a> <a class="button secondary" href="guides/tetris.html">Build Tetris</a> <a class="button secondary" href="pkg/engine.html">API reference</a></p>
</div>
<div class="cards">
<a class="card" href="pkg/gfx.html"><h3>Rendering</h3><p>Sprites, tilemaps and scalable text on top of a physically based 3D renderer with cascaded shadows, ambient occlusion, bloom and skeletal animation. No Vulkan knowledge required.</p></a>
<a class="card" href="pkg/ui.html"><h3>Interface</h3><p>An immediate-mode toolkit rebuilt every frame: panels, buttons, sliders, drop-downs, scroll areas, text fields with IME support, eight colour themes and texture skins.</p></a>
<a class="card" href="pkg/audio.html"><h3>Audio</h3><p>A mixer with streamed music, positional voices, reverb and filters, priorities and fades, plus a tracker player for MOD, S3M, XM and IT.</p></a>
<a class="card" href="pkg/ecs.html"><h3>Game services</h3><p>An archetype-based entity component system with systems, resources and events, assets with packs and hot reload, saves and settings, seeded random numbers, timers and tweens, grids with pathfinding, and TCP and UDP messaging.</p></a>
</div>
<h2>Guides</h2>
{{range .Site.GuideGroups}}<h3>{{.Title}}</h3><ul class="guide-list">
{{range .Guides}}<li><a href="guides/{{.Slug}}.html">{{.Title}}</a>{{if .Summary}} <span class="dim">— {{.Summary}}</span>{{end}}</li>
{{end}}</ul>
{{end}}{{if .Site.Programs}}<h2>Example programs</h2>
<p>Screenshot-capable examples run headless; the window smoke test requires a desktop. Every example has a <a href="examples/index.html">walkthrough</a> that explains its source section by section.</p>
<ul class="guide-list">
{{range .Site.Programs}}<li><a href="{{.URL}}">{{.Title}}</a>{{if .Summary}} <span class="dim">— {{.Summary}}</span>{{end}}</li>
{{end}}</ul>
{{end}}<h2>Packages</h2>
{{range .Site.Groups}}<h3>{{.Title}}</h3><table class="pkgs">
{{range .Packages}}<tr><td><a href="{{.URL}}">{{short .Rel}}</a></td><td>{{.Synopsis}}</td></tr>
{{end}}</table>
{{end}}
{{end}}`

const guideTmpl = `{{define "guide"}}{{template "layout" .}}{{end}}
{{define "guideBody"}}
<article class="guide">
{{if .Guide.Headings}}<aside class="toc"><h4>On this page</h4><ul>{{range .Guide.Headings}}<li><a href="#{{.ID}}">{{.Text}}</a></li>{{end}}</ul></aside>{{end}}
<h1>{{.Guide.Title}}</h1>
{{.Guide.Body}}
</article>
{{end}}`

const programTmpl = `{{define "program"}}{{template "layout" .}}{{end}}
{{define "programBody"}}
{{$p := .Program}}
<article class="guide">
{{if $p.Headings}}<aside class="toc"><h4>On this page</h4><ul>{{range $p.Headings}}<li><a href="#{{.ID}}">{{.Text}}</a></li>{{end}}</ul></aside>{{end}}
<p class="crumbs">Example <code>examples/{{$p.Name}}</code></p>
<h1>{{$p.Title}}</h1>
{{if $p.Shot}}<p class="shot"><img src="{{$p.Name}}.png" alt="{{$p.Title}}"></p>{{end}}
{{if $p.Missing}}<p>The walkthrough for this example is not written yet. Read the source until it is.</p>{{end}}
{{$p.Body}}
<h2 id="files">Source files</h2>
<p class="files">{{range $p.Files}}<a href="` + repo + `/blob/main/examples/{{$p.Name}}/{{.}}">{{.}}</a> {{end}}</p>
<p><a href="{{$p.SourceURL}}">The whole directory on GitHub</a></p>
</article>
{{end}}
{{define "programIndex"}}{{template "layout" .}}{{end}}
{{define "programIndexBody"}}
<article class="guide">
<h1>Example programs</h1>
<p>One directory per example under <code>examples/</code>. Screenshot-capable examples take <code>-seconds N</code> and <code>-shot file.png</code> for headless verification. The <code>window</code> smoke test requires a desktop. Every example has a walkthrough that explains its source section by section.</p>
<ul class="guide-list">
{{range .Site.Programs}}<li><a href="{{.Name}}.html">{{.Title}}</a>{{if .Summary}} <span class="dim">— {{.Summary}}</span>{{end}}</li>
{{end}}</ul>
</article>
{{end}}`

const packageTmpl = `{{define "package"}}{{template "layout" .}}{{end}}
{{define "example"}}<details class="example" open><summary>Example{{if .Suffix}} ({{.Suffix}}){{end}}</summary>
{{.Doc}}<pre class="code"><code>{{.Code}}</code></pre>
{{if .Output}}<div class="output"><span>Output</span><pre>{{.Output}}</pre></div>{{end}}</details>{{end}}
{{define "func"}}<div class="decl" id="{{.ID}}">
<h4><a class="anchor" href="#{{.ID}}">{{.Name}}</a> <a class="src" href="{{.Src}}">source</a></h4>
<pre class="code sig"><code>{{.Decl}}</code></pre>
{{.Doc}}
{{range .Examples}}{{template "example" .}}{{end}}
</div>{{end}}
{{define "value"}}<div class="decl">
{{range .Names}}<a id="{{.}}"></a>{{end}}
<pre class="code"><code>{{.Decl}}</code></pre>
{{.Doc}}
</div>{{end}}
{{define "packageBody"}}
{{$p := .Package}}
<article class="package">
<p class="crumbs">{{if $p.IsCommand}}Command{{else}}Package{{end}} <code>{{$p.ImportPath}}</code></p>
<h1>{{short $p.Rel}}</h1>
{{$p.Doc}}
{{if or $p.Consts $p.Vars $p.Funcs $p.Types}}
<h2 id="index">Index</h2>
<ul class="index">
{{if $p.Consts}}<li><a href="#constants">Constants</a></li>{{end}}
{{if $p.Vars}}<li><a href="#variables">Variables</a></li>{{end}}
{{range $p.Funcs}}<li><a href="#{{.ID}}"><code>{{.Decl}}</code></a></li>{{end}}
{{range $p.Types}}<li><a href="#{{.Name}}">type {{.Name}}</a>
{{if or .Funcs .Methods}}<ul>{{range .Funcs}}<li><a href="#{{.ID}}"><code>{{.Decl}}</code></a></li>{{end}}
{{range .Methods}}<li><a href="#{{.ID}}"><code>{{.Decl}}</code></a></li>{{end}}</ul>{{end}}</li>{{end}}
</ul>
{{end}}
{{if $p.Examples}}<h2 id="examples">Examples</h2>{{range $p.Examples}}{{template "example" .}}{{end}}{{end}}
{{if $p.Consts}}<h2 id="constants">Constants</h2>{{range $p.Consts}}{{template "value" .}}{{end}}{{end}}
{{if $p.Vars}}<h2 id="variables">Variables</h2>{{range $p.Vars}}{{template "value" .}}{{end}}{{end}}
{{if $p.Funcs}}<h2 id="functions">Functions</h2>{{range $p.Funcs}}{{template "func" .}}{{end}}{{end}}
{{if $p.Types}}<h2 id="types">Types</h2>
{{range $p.Types}}<div class="type" id="{{.Name}}">
{{range .Members}}<a id="{{.Name}}"></a>{{end}}
<h3><a class="anchor" href="#{{.Name}}">type {{.Name}}</a> <a class="src" href="{{.Src}}">source</a></h3>
<pre class="code"><code>{{.Decl}}</code></pre>
{{.Doc}}
{{range .Examples}}{{template "example" .}}{{end}}
{{range .Consts}}{{template "value" .}}{{end}}
{{range .Vars}}{{template "value" .}}{{end}}
{{range .Funcs}}{{template "func" .}}{{end}}
{{range .Methods}}{{template "func" .}}{{end}}
</div>{{end}}{{end}}
<h2 id="files">Source files</h2>
<p class="files">{{range $p.Files}}<a href="` + repo + `/blob/main/{{$p.Rel}}{{if $p.Rel}}/{{end}}{{.}}">{{.}}</a> {{end}}</p>
</article>
{{end}}`
