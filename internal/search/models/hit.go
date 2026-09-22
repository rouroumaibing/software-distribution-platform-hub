package models

import "github.com/google/uuid"

// 资源类型常量。取值是**跨层契约**：console 的 `SearchHit.kind`（src/utils/search.ts）
// 与跳转表 `searchHitRoute` 都按这三个字面量分支，改这里必须同步改前端。
const (
	TypeService   = "service"
	TypeComponent = "component"
	TypePipeline  = "pipeline"
)

// AllTypes 是 type 省略时的默认搜索域，顺序即响应里的分组顺序。
var AllTypes = []string{TypeService, TypeComponent, TypePipeline}

// IsValidType reports whether t is one of the searchable resource types.
func IsValidType(t string) bool {
	for _, v := range AllTypes {
		if v == t {
			return true
		}
	}
	return false
}

// Hit is one cross-resource search result.
//
// 契约来源：CONSOLE-UI-DESIGN.md §5.3 / 附 A N-8 —— `GET /search?q=&type=&limit=`
// 返回 `{type,name,path,id}`。字段名刻意与前端 SearchHit 对齐（type↔kind 由
// console 的 api 层翻译），这样前端拿到就能直接喂进既有的打分/键盘导航逻辑。
//
//   - Path 是**展示用**的所属层级（`组织 / 服务`、`组织 / 服务 / 组件`），不是 URL。
//     §4.1 要求「结果行必须显示所属路径」，否则两个不同服务下的同名组件无法区分
//     —— 这不是装饰，是可用性硬约束。
//   - Keyword 参与匹配但不展示（service.key / component.key），与前端 SearchHit.keyword 同义。
type Hit struct {
	Type    string    `json:"type"`
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Keyword string    `json:"keyword,omitempty"`
}
