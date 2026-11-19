package dogerhttp

import (
	"html/template"
)

// pages is the template set of every page of the debug UI. It is parsed
// once. html/template escapes every name as text and every link as a URL,
// so no value of a scope, a service or a base path can inject markup.
var pages = template.Must(template.New("pages").Parse(pagesTemplate))

const pagesTemplate = `
{{- define "menu" -}}
		<small>
			Menu:
			<a href="{{.Home}}">Home</a>
			-
			<a href="{{.Scopes}}">Scopes</a>
			-
			<a href="{{.Services}}">Services</a>
		</small>
{{- end -}}

{{- define "index" -}}
<!DOCTYPE html>
<html>
	<head>
		<title>Dependency injection UI - doger</title>
	</head>
	<body>
		<h1>Welcome to doger UI ✌️</h1>

		<h2>Introduction</h2>
		<ul>
			<li><a href="https://github.com/kartaladev/doger" target="_blank">Repository</a></li>
			<li><a href="https://github.com/kartaladev/doger/issues" target="_blank">New issue</a></li>
			<li><a href="https://kartaladev.github.io/doger/" target="_blank">Documentation</a></li>
			<li><a href="https://pkg.go.dev/github.com/kartaladev/doger" target="_blank">Godoc</a></li>
			<li><a href="https://github.com/kartaladev/doger/releases" target="_blank">Changelog</a></li>
			<li><a href="https://github.com/kartaladev/doger/blob/main/LICENSE" target="_blank">License</a></li>
			<li><a href="https://github.com/samber/do" target="_blank">Upstream project</a></li>
		</ul>

		<h2>Getting started</h2>
		<ul>
			<li><a href="{{.Menu.Scopes}}">Inspect scopes</a></li>
			<li><a href="{{.Menu.Services}}">Inspect services</a></li>
		</ul>
	</body>
</html>
{{- end -}}

{{- define "serviceEntry" -}}
			{{.Icon}}
			<a href="{{.Link}}">
				{{.Name}}
			</a>
			{{.Features}}
{{- end -}}

{{- define "scopeEntry" -}}
			Scope:
			<a href="{{.Link}}">
				{{.Name}}
			</a>

			{{if .Services}}
				<ul class="services">
					{{range .Services}}
						<li class="service">
							{{template "serviceEntry" .}}
						</li>
					{{end}}
				</ul>
			{{end}}

			{{if .Children}}
				<ul class="scopes">
					{{range .Children}}
						<li class="scope">
							{{template "scopeEntry" .}}
						</li>
					{{end}}
				</ul>
			{{end}}
{{- end -}}

{{- define "scopeTree" -}}
<!DOCTYPE html>
<html>
	<head>
		<title>Inspect scope tree - doger</title>
		<style>
		header {
			margin-bottom: 40px;
		}
		.scopes {
			margin-left: 10px;
			padding-top: 10px;
			padding-bottom: 10px;
			padding-left: 30px;
			border-left: 2px solid red;
		}
		.services {
			padding-top: 5px;
			padding-bottom: 5px;
		}
		</style>
	</head>
	<body>
		<h1>Scope description</h1>
		{{template "menu" .Menu}}

		<header>
			<p>
				<b>Spec</b>:
				<br><br>
				😴 Lazy service
				<br>
				🔁 Eager service
				<br>
				🏭 Transient service
				<br>
				🔗 Service alias
				<br>
				🫀 Implements Healthchecker
				<br>
				🙅 Implements Shutdowner
			</p>
		</header>

		{{if .Scopes}}
			<ul class="scopes">
				{{range .Scopes}}
					<li class="scope">
						{{template "scopeEntry" .}}
					</li>
				{{end}}
			</ul>
		{{end}}
	</body>
</html>
{{- end -}}

{{- define "serviceList" -}}
<!DOCTYPE html>
<html>
	<head>
		<title>Inspect service - doger</title>
		<style>
		.scope {
			padding-top: 10px;
			padding-bottom: 10px;
		}
		</style>
	</head>
	<body>
		<h1>Service description</h1>
		{{template "menu" .Menu}}

		<ul class="scopes">
			{{range .Scopes}}
				<li class="scope">
					{{template "scopeEntry" .}}
				</li>
			{{end}}
		</ul>
	</body>
</html>
{{- end -}}

{{- define "dependencyTree" -}}
		<ul class="services">
			{{range .}}
				<li class="service">
					<a href="{{.Link}}">
						{{.Name}}
					</a>
					{{if .Children}}
						{{template "dependencyTree" .Children}}
					{{end}}
				</li>
			{{end}}
		</ul>
{{- end -}}

{{- define "service" -}}
<!DOCTYPE html>
<html>
<head>
	<title>Inspect service - doger</title>
	<style>
	</style>
</head>
<body>
	<h1>Services by scope</h1>
	{{template "menu" .Menu}}

	<p>
		Scope id: {{.ScopeID}}
		<br>
		Scope name: {{.ScopeName}}
		<br>
		Service name: {{.ServiceName}}
		<br>
		Service type: {{.ServiceType}}
		{{if .ServiceBuildTime}}
			<br>
			Service build time: {{.ServiceBuildTime}}
		{{end}}
		<br>
		Invoked at: {{.Invoked}}
	</p>

	<h2>Dependencies:</h2>
	{{template "dependencyTree" .Dependencies}}

	<h2>Dependents:</h2>
	{{template "dependencyTree" .Dependents}}
</body>
</html>
{{- end -}}
`
