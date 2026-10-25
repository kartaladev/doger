package dogerhttp

import (
	"bytes"
	"text/template"

	"github.com/kartaladev/doger"
)

func fromTemplate(tpl string, data any) (string, error) {
	t := template.Must(template.New("").Parse(tpl))
	var buf bytes.Buffer

	err := t.Execute(&buf, data) // 🤮
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func mAp[T any, R any](collection []T, iteratee func(T) R) []R {
	result := make([]R, len(collection))

	for i, item := range collection {
		result[i] = iteratee(item)
	}

	return result
}

func getScopeByID(injector doger.Injector, id string) (doger.ExplainInjectorScopeOutput, bool) {
	scopes := getAllScopes(injector)
	for _, scope := range scopes {
		if scope.ScopeID == id {
			return scope, true
		}
	}
	return doger.ExplainInjectorScopeOutput{}, false
}

func getAllScopes(injector doger.Injector) []doger.ExplainInjectorScopeOutput {
	description := doger.ExplainInjector(injector)

	return getAllScopesRec(description.DAG)
}

func getAllScopesRec(scopes []doger.ExplainInjectorScopeOutput) []doger.ExplainInjectorScopeOutput {
	output := make([]doger.ExplainInjectorScopeOutput, 0, len(scopes))
	for i := range scopes {
		output = append(output, scopes[i])
		output = append(output, getAllScopesRec(scopes[i].Children)...)
	}
	return output
}
