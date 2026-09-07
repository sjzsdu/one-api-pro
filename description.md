## User request

web上需要有个测验功能，用户输入一个prompt的时候，按照现有的策略，选出什么model。让用户有一个直观的感受，智能筛选的工作效果。但是不需要真实的访问大模型。

## Additional request

我希望在嵌入这个模式时，程序能够自动的去下载对应的 ONNX 文件保存到本地，这样就免去再 env 中配置了，env 只要指定模型名称就可以。

当前 env 配置（`MODEL_ROUTER_STRATEGY=embedding` 时生效）需要手动指定多个路径：

```
EMBEDDING_PROVIDER=onnx
EMBEDDING_MODEL=jina-v2-code
EMBEDDING_MODEL_PATH=/models/model.onnx
EMBEDDING_TOKENIZER_PATH=/models/tokenizer.json
ONNXRUNTIME_LIBRARY=/opt/onnxruntime/lib/libonnxruntime.dylib
```

期望行为：用户只需配置 `EMBEDDING_MODEL=jina-v2-code`，程序自动根据模型名查找并下载对应的 ONNX 模型文件、tokenizer.json 等依赖到本地缓存目录，无需手动指定 `EMBEDDING_MODEL_PATH`、`EMBEDDING_TOKENIZER_PATH`、`ONNXRUNTIME_LIBRARY` 等路径。

## Context

（无额外上下文）
