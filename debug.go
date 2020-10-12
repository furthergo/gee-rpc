package gee_rpc

import (
	"fmt"
	"html/template"
	"net/http"
)

const debugText = `<html>
	<body>
	<title>GeeRPC Services</title>
	{{range .}}
	<hr>
	Service {{.Name}}
	<hr>
		<table>
		<th align=center>Method</th><th align=center>Calls</th>
		{{range $Name, $mtype := .Method}}
			<tr>
			<td align=left font=fixed>{{$Name}}({{$mtype.ArgType}}, {{$mtype.ReplyType}}) error</td>
			<td align=center>{{$mtype.NumCalls}}</td>
			</tr>
		{{end}}
		</table>
	{{end}}
	</body>
	</html>`

var debug = template.Must(template.New("debugRPC").Parse(debugText))

type debugHTTP struct {
	*Server
}

type debugService struct {
	Name string
	Method map[string]*methodType
}

func (s debugHTTP)ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var services []debugService
	s.serviceMap.Range(func(key, value interface{}) bool {
		svc := value.(*service)
		services = append(services, debugService{
			Name: svc.Name,
			Method: svc.method,
		})
		return true
	})
	err := debug.Execute(w, services)
	if err != nil {
		_, _ = fmt.Fprintln(w, "rpc: error executing template:", err.Error())
	}
}

