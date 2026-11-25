export function statusTone(status: string) {
  if (['就绪', '打包通过', '已发布', '已保存草稿', '可检索'].includes(status))
    return 'success'
  if (['上传失败', '发布失败', '处理失败'].includes(status)) return 'error'
  if (['待核对', '发布待确认', '仅保存草稿', '未执行'].includes(status)) return 'warning'
  return 'neutral'
}
