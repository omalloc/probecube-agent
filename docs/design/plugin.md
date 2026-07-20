# 拨测插件实现

## 1. 插件实现要求

> 需要实现 `engine.Prober` 接口

```go

var _ engine.Prober = (*CustomProber)(nil)

func init() {
  // 注册插件
  engine.RegisterProber(engine.ProtocolHTTP, setup())
}

func setup() *CustomProber {
  return &CustomProber{}
}

type CustomProber struct {
  // 可添加插件特有的配置项
}

func (r *CustomProber) Probe(ctx context.Context, task *engine.Task) (*engine.Result, error) {
  // 实现拨测逻辑
  // ...
  return &engine.Result{
    TaskID: task.ID,
    Status: engine.StatusSuccess,
    Data:   "拨测结果数据",
  }, nil
}

```