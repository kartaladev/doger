package dogerhttp

// IndexHTML generates the main HTML page for the doger library's web-based debugging interface.
// This function creates a welcome page that provides navigation to various debugging features
// and links to external resources like documentation and the GitHub repository.
//
// Parameters:
//   - basePath: The base URL path for the web interface (e.g., "/debug/di")
//   - opts: Page options; none changes this page
//
// Returns the HTML content as a string and any error that occurred during generation.
//
// The generated page includes:
//   - Links to external resources (GitHub, documentation, etc.)
//   - Navigation to scope and service inspection pages
//   - Introduction to the debugging interface
//
// The base path is escaped as a URL path in every link.
//
// Example:
//
//	html, err := http.IndexHTML("/debug/di")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Fprint(w, html)
func IndexHTML(basePath string, opts ...Option) (string, error) {
	return render("index", struct{ Menu menuLinks }{Menu: newMenuLinks(basePath)})
}
