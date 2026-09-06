<template>
  <div class="model-quiz-page">
    <div class="quiz-header">
      <h1>模型路由测验</h1>
      <p class="subtitle">输入 Prompt，查看智能路由会选择哪个模型</p>
    </div>

    <div class="quiz-content">
      <div class="input-section">
        <a-textarea
          v-model="prompt"
          placeholder="请输入你的 Prompt，例如：帮我写一个 Python 函数来排序列表"
          :auto-size="{ minRows: 3, maxRows: 8 }"
          :disabled="loading"
          @keydown.ctrl.enter="analyzePrompt"
          @keydown.meta.enter="analyzePrompt"
        />
        <div class="input-actions">
          <a-space>
            <a-button @click="fillExample('code')">
              代码示例
            </a-button>
            <a-button @click="fillExample('translate')">
              翻译示例
            </a-button>
            <a-button @click="fillExample('math')">
              数学示例
            </a-button>
            <a-button @click="fillExample('creative')">
              创意示例
            </a-button>
            <a-button @click="fillExample('chat')">
              聊天示例
            </a-button>
          </a-space>
          <a-button
            type="primary"
            :loading="loading"
            :disabled="!prompt.trim()"
            @click="analyzePrompt"
          >
            分析路由
          </a-button>
        </div>
      </div>

      <div v-if="result" class="result-section">
        <a-divider />

        <div class="result-header">
          <h2>路由结果</h2>
          <a-tag :color="getCategoryColor(result.detected_category)">
            {{ getCategoryLabel(result.detected_category) || '未识别' }}
          </a-tag>
        </div>

        <a-row :gutter="24">
          <a-col :xs="24" :md="12">
            <div class="result-card selected-model">
              <div class="card-label">选中模型</div>
              <div class="card-value model-name">{{ result.selected_model }}</div>
              <div class="card-reason">{{ result.reason }}</div>
            </div>
          </a-col>
          <a-col :xs="24" :md="12">
            <div class="result-card turn-type">
              <div class="card-label">请求类型</div>
              <div class="card-value">{{ getTurnTypeLabel(result.turn_type) }}</div>
            </div>
          </a-col>
        </a-row>

        <div class="scores-section">
          <h3>模型评分</h3>
          <div class="scores-grid">
            <div
              v-for="item in sortedScores"
              :key="item.model"
              class="score-item"
              :class="{ 'highest': item.model === result.selected_model }"
            >
              <div class="score-model">{{ item.model }}</div>
              <div class="score-bar-wrapper">
                <div
                  class="score-bar"
                  :style="{ width: (item.score / maxScore * 100) + '%' }"
                />
              </div>
              <div class="score-value">{{ item.score.toFixed(1) }}</div>
            </div>
          </div>
        </div>

        <div class="info-section">
          <a-collapse>
            <a-collapse-item header="详细信息" key="details">
              <div class="detail-item">
                <span class="detail-label">可用模型数：</span>
                <span>{{ result.available_models?.length || 0 }}</span>
              </div>
              <div class="detail-item">
                <span class="detail-label">过滤掉的模型：</span>
                <span>{{ result.filtered_out_models?.length || 0 }}</span>
              </div>
              <div class="detail-item">
                <span class="detail-label">使用策略：</span>
                <span>{{ result.strategy }}</span>
              </div>
            </a-collapse-item>
          </a-collapse>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { Message } from '@arco-design/web-vue'
import api from '@/api'

const prompt = ref('')
const loading = ref(false)
const result = ref(null)

const sortedScores = computed(() => Object.entries(result.value?.model_scores || {})
  .map(([model, score]) => ({ model, score }))
  .sort((a, b) => b.score - a.score || a.model.localeCompare(b.model)))
const maxScore = computed(() => Math.max(1, ...sortedScores.value.map(item => item.score)))

const examples = {
  code: '帮我写一个 Python 函数来实现快速排序算法',
  translate: '请将以下英文翻译成中文：Hello, how are you today?',
  math: '证明勾股定理，并计算直角三角形的面积',
  creative: '写一首关于秋天的诗，要有意境',
  chat: '你好，你是谁？',
}

function fillExample(type) {
  prompt.value = examples[type] || ''
}

async function analyzePrompt() {
  if (!prompt.value.trim()) {
    Message.warning('请输入 Prompt')
    return
  }

  loading.value = true
  try {
    const response = await api.post('/api/model_router/quiz', {
      prompt: prompt.value.trim(),
    })
    if (response.data.success) {
      result.value = response.data.data
    } else {
      Message.error(response.data.message || '分析失败')
    }
  } catch (error) {
    Message.error('请求失败：' + (error.response?.data?.message || error.message))
  } finally {
    loading.value = false
  }
}

function getCategoryColor(category) {
  const colors = {
    code: 'blue',
    translate: 'green',
    math: 'orange',
    reason: 'purple',
    creative: 'pink',
    chat: 'cyan',
  }
  return colors[category] || 'gray'
}

function getCategoryLabel(category) {
  const labels = {
    code: '代码',
    translate: '翻译',
    math: '数学',
    reason: '推理',
    creative: '创意',
    chat: '聊天',
  }
  return labels[category] || category
}

function getTurnTypeLabel(turnType) {
  const labels = {
    normal: '普通对话',
    compression: '上下文压缩',
    sub_agent: '子智能体',
    tool_result: '工具结果',
    title_generation: '标题生成',
  }
  return labels[turnType] || turnType
}
</script>

<style scoped>
.model-quiz-page {
  padding: 24px;
  max-width: 900px;
  margin: 0 auto;
}

.quiz-header {
  text-align: center;
  margin-bottom: 32px;
}

.quiz-header h1 {
  margin: 0 0 8px 0;
  font-size: 28px;
  color: #1d2129;
}

.subtitle {
  margin: 0;
  color: #86909c;
  font-size: 14px;
}

.input-section {
  margin-bottom: 24px;
}

.input-actions {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 12px;
}

.input-actions :deep(.arco-space) {
  flex-wrap: wrap;
}

.result-section {
  animation: fadeIn 0.3s ease;
}

@keyframes fadeIn {
  from {
    opacity: 0;
    transform: translateY(10px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.result-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 20px;
}

.result-header h2 {
  margin: 0;
  font-size: 20px;
}

.result-card {
  background: #f7f8fa;
  border-radius: 8px;
  padding: 20px;
  margin-bottom: 16px;
}

.result-card.selected-model {
  background: linear-gradient(135deg, #e8f3ff 0%, #f0f5ff 100%);
  border: 1px solid #bedaff;
}

.card-label {
  font-size: 12px;
  color: #86909c;
  margin-bottom: 8px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.card-value {
  font-size: 18px;
  font-weight: 600;
  color: #1d2129;
}

.card-value.model-name {
  color: #165dff;
  font-size: 24px;
}

.card-reason {
  margin-top: 12px;
  font-size: 13px;
  color: #4e5969;
  line-height: 1.6;
}

.scores-section {
  margin: 24px 0;
}

.scores-section h3 {
  margin: 0 0 16px 0;
  font-size: 16px;
  color: #1d2129;
}

.scores-grid {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.score-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 12px;
  border-radius: 6px;
  transition: background-color 0.2s;
}

.score-item:hover {
  background: #f7f8fa;
}

.score-item.highest {
  background: #e8f3ff;
  border: 1px solid #bedaff;
}

.score-model {
  width: 140px;
  font-size: 13px;
  font-weight: 500;
  color: #1d2129;
}

.score-bar-wrapper {
  flex: 1;
  height: 8px;
  background: #e5e6eb;
  border-radius: 4px;
  overflow: hidden;
}

.score-bar {
  height: 100%;
  background: linear-gradient(90deg, #165dff 0%, #4080ff 100%);
  border-radius: 4px;
  transition: width 0.3s ease;
}

.score-value {
  width: 40px;
  text-align: right;
  font-size: 13px;
  font-weight: 600;
  color: #1d2129;
}

.info-section {
  margin-top: 24px;
}

.detail-item {
  display: flex;
  padding: 8px 0;
  border-bottom: 1px solid #e5e6eb;
}

.detail-item:last-child {
  border-bottom: none;
}

.detail-label {
  color: #86909c;
  margin-right: 8px;
}

@media (max-width: 640px) {
  .model-quiz-page {
    padding: 16px;
  }

  .input-actions {
    align-items: stretch;
    flex-direction: column;
    gap: 12px;
  }

  .score-model {
    width: 105px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}
</style>
