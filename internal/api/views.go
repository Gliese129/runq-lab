package api

import (
	"context"
	"net/url"

	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/views"
)

func viewPath(project, name string) string {
	return "/api/v1/projects/" + url.PathEscape(project) + "/views/" + url.PathEscape(name)
}
func (p *Proxy) ListViews(ctx context.Context, project string) ([]store.ViewSummary, error) {
	path := "/api/v1/views"
	if project != "" {
		path = "/api/v1/projects/" + url.PathEscape(project) + "/views"
	}
	var out []store.ViewSummary
	err := p.do(ctx, "GET", path, nil, &out)
	return out, err
}
func (p *Proxy) SaveView(ctx context.Context, v store.SavedView) error {
	return p.do(ctx, "PUT", viewPath(v.Spec.Project, v.Spec.Name), v, nil)
}
func (p *Proxy) GetView(ctx context.Context, project, name string) (*store.SavedView, error) {
	var v store.SavedView
	err := p.do(ctx, "GET", viewPath(project, name), nil, &v)
	return &v, err
}
func (p *Proxy) DeleteView(ctx context.Context, project, name string) error {
	return p.do(ctx, "DELETE", viewPath(project, name), nil, nil)
}
func (p *Proxy) RenderView(ctx context.Context, project, name string, jobs []string, refresh bool) (*views.Result, error) {
	q := url.Values{}
	for _, id := range jobs {
		q.Add("job", id)
	}
	if refresh {
		q.Set("refresh", "true")
	}
	var out views.Result
	err := p.do(ctx, "GET", viewPath(project, name)+"/render?"+q.Encode(), nil, &out)
	return &out, err
}

// RenderJobView renders a job with no saved view (GET /jobs/{id}/view);
// the service resolves a short id.
func (p *Proxy) RenderJobView(ctx context.Context, job string) (*views.JobView, error) {
	var out views.JobView
	err := p.do(ctx, "GET", "/api/v1/jobs/"+url.PathEscape(job)+"/view", nil, &out)
	return &out, err
}
