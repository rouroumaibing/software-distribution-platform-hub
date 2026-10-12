package service

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
)

// ui-12 §5.3（N-26）：PUT /pipelines/:id 必须把 params（运行时参数声明）
// 一并落到既有行上 —— 早前 Update 只拷贝 name/kind/description，编辑器
// 声明的参数会静默丢失，触发弹窗也就永远渲染不出声明表单。
// 发布路径（versions != nil）与非发布路径（原地自增）都要带 params。

type paramsCaptureStore struct {
	row     *models.Pipeline
	updates int
}

func (f *paramsCaptureStore) Create(*models.Pipeline) error { return nil }

func (f *paramsCaptureStore) GetByID(uuid.UUID) (*models.Pipeline, error) {
	return f.row, nil
}

func (f *paramsCaptureStore) Update(p *models.Pipeline) error {
	f.updates++
	f.row = p
	return nil
}

func (f *paramsCaptureStore) Delete(uuid.UUID) error { return nil }

func (f *paramsCaptureStore) FindByComponentID(uuid.UUID, common.Pagination) ([]models.Pipeline, int64, error) {
	return nil, 0, nil
}

func (f *paramsCaptureStore) FindAll(uuid.UUID, string, string, common.Pagination) ([]models.Pipeline, int64, error) {
	return nil, 0, nil
}

func newParamsTestService(store *paramsCaptureStore, versions VersionPublisher) *PipelineService {
	return NewPipelineService(store, versions, &fakeRunExistence{})
}

func declsJSON(t *testing.T, body string) datatypes.JSON {
	t.Helper()
	if !strings.Contains(body, `"name"`) {
		t.Fatalf("fixture 应是 ParamDecl 数组，得到: %s", body)
	}
	return datatypes.JSON(body)
}

func newPipelineRow(name, kind string) *models.Pipeline {
	return &models.Pipeline{ID: uuid.New(), Name: name, Kind: kind}
}

func TestPipelineUpdate_CopiesParams(t *testing.T) {
	row := newPipelineRow("old", "build")
	store := &paramsCaptureStore{row: row}
	svc := newParamsTestService(store, nil)

	in := &models.Pipeline{Name: "new", Kind: "release", Params: declsJSON(t, `[{"name":"VERSION","type":"string","required":true}]`)}
	if err := svc.Update(row.ID, in); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if store.updates != 1 {
		t.Fatalf("Update 应落库一次，得到 %d 次", store.updates)
	}
	got := models.DecodeParamDecls(store.row.Params)
	if len(got) != 1 || got[0].Name != "VERSION" || !got[0].Required {
		t.Fatalf("params 未透传或内容不符: %+v", got)
	}
	if store.row.Name != "new" || store.row.Kind != "release" {
		t.Fatalf("标量字段拷贝被破坏: %+v", store.row)
	}
}

func TestPipelineUpdate_WithPublisherCopiesParams(t *testing.T) {
	row := newPipelineRow("old", "build")
	store := &paramsCaptureStore{row: row}
	pub := &fakeVersionPublisher{}
	svc := newParamsTestService(store, pub)

	in := &models.Pipeline{Name: "new", Kind: "build", Params: declsJSON(t, `[{"name":"ENV","type":"choice","options":["dev","prod"]}]`)}
	if err := svc.Update(row.ID, in); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(pub.published) != 1 {
		t.Fatalf("发布路径应触发一次 Publish，得到 %d", len(pub.published))
	}
	got := models.DecodeParamDecls(store.row.Params)
	if len(got) != 1 || got[0].Name != "ENV" || len(got[0].Options) != 2 {
		t.Fatalf("发布路径 params 未透传: %+v", got)
	}
}

func TestPipelineUpdate_ClearsParams(t *testing.T) {
	// 声明清空是合法编辑：旧声明不应残留。
	row := newPipelineRow("old", "build")
	row.Params = declsJSON(t, `[{"name":"OLD"}]`)
	store := &paramsCaptureStore{row: row}
	svc := newParamsTestService(store, nil)

	in := &models.Pipeline{Name: "old", Kind: "build"}
	if err := svc.Update(row.ID, in); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(models.DecodeParamDecls(store.row.Params)) != 0 {
		t.Fatalf("清空声明未生效，仍残留: %s", string(store.row.Params))
	}
}
