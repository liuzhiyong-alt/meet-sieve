let activeModalTrigger = null;
let playingButton = null;
const dirtyTextInputs = new Set();
const dirtySpeakerMaps = new Set();
const dirtySpeakerOverrides = new Set();

/** showToast 展示原型中的短暂交互反馈。 */
function showToast(message) {
  const toast = document.querySelector("[data-toast]");
  if (!toast) return;
  toast.textContent = message;
  toast.classList.add("show");
  window.clearTimeout(showToast.timer);
  showToast.timer = window.setTimeout(() => toast.classList.remove("show"), 2200);
}

/** openModal 打开指定弹窗并把焦点移到第一个操作。 */
function openModal(id, trigger) {
  const modal = document.getElementById(id);
  if (!modal) return;
  activeModalTrigger = trigger;
  modal.classList.add("open");
  modal.querySelector("button, a, input, select")?.focus();
}

/** closeModal 关闭弹窗并恢复触发位置的焦点。 */
function closeModal(modal) {
  modal?.classList.remove("open");
  activeModalTrigger?.focus();
}

/** getDirtyCount 返回当前页面全部未保存修改数量。 */
function getDirtyCount() {
  return dirtyTextInputs.size + dirtySpeakerMaps.size + dirtySpeakerOverrides.size;
}

/** updateSaveSummary 根据是否有修改同步保存按钮状态。 */
function updateSaveSummary() {
  const save = document.querySelector("[data-save-all]");
  if (!save) return;
  const dirtyCount = getDirtyCount();
  save.disabled = dirtyCount === 0;
}

/** resizeTextarea 根据正文内容自动调整输入框高度。 */
function resizeTextarea(textarea) {
  textarea.style.height = "auto";
  textarea.style.height = `${textarea.scrollHeight}px`;
}

/** updateTranscriptDirtyState 更新单条记录的未保存视觉状态。 */
function updateTranscriptDirtyState(item) {
  const input = item.querySelector("[data-transcript-input]");
  const speaker = item.querySelector("[data-segment-speaker]");
  const dirty = dirtyTextInputs.has(input) || dirtySpeakerOverrides.has(speaker);
  item.classList.toggle("is-dirty", dirty);
}

/** markTextInputDirty 根据已保存值判断正文是否存在修改。 */
function markTextInputDirty(textarea) {
  const item = textarea.closest("[data-transcript]");
  const changed = textarea.value !== textarea.dataset.savedValue;
  if (changed) dirtyTextInputs.add(textarea);
  else dirtyTextInputs.delete(textarea);
  if (changed) {
    const status = item?.querySelector(".status");
    if (status) {
      status.textContent = "文字已修改";
      status.className = "status info";
    }
  }
  if (item) updateTranscriptDirtyState(item);
  updateSaveSummary();
}

/** initTranscriptInputs 初始化始终可编辑且自动增高的正文输入框。 */
function initTranscriptInputs() {
  const inputs = [...document.querySelectorAll("[data-transcript-input]")];
  inputs.forEach((textarea) => {
    textarea.dataset.savedValue ??= textarea.value;
    resizeTextarea(textarea);
    markTextInputDirty(textarea);
    textarea.addEventListener("input", () => {
      resizeTextarea(textarea);
      markTextInputDirty(textarea);
    });
  });

  window.addEventListener("resize", () => inputs.forEach(resizeTextarea));
}

/** updateClusterPreview 把本场说话人选择投影到未单独修改的记录。 */
function updateClusterPreview(select) {
  const cluster = select.dataset.speakerMap;
  const selectedName = select.value;
  document.querySelectorAll(`[data-transcript][data-cluster="${cluster}"]`).forEach((item) => {
    const segmentSelect = item.querySelector("[data-segment-speaker]");
    if (!segmentSelect || segmentSelect.dataset.userOverride === "true") return;
    segmentSelect.value = selectedName;
  });

  if (select.value === select.dataset.savedValue) dirtySpeakerMaps.delete(select);
  else dirtySpeakerMaps.add(select);
  updateSpeakerStatus();
  updateSaveSummary();
}

/** updateSpeakerStatus 根据未指定聚类数量更新页面状态。 */
function updateSpeakerStatus() {
  const unresolved = [...document.querySelectorAll("[data-speaker-map]")].filter(
    (select) => select.selectedIndex === 0,
  ).length;
  const text = unresolved ? `${unresolved} 位说话人待确认` : "本场说话人已确认";
  document.querySelectorAll("[data-page-status], [data-sidebar-status]").forEach((status) => {
    status.textContent = text;
    status.classList.toggle("warning", unresolved > 0);
    status.classList.toggle("success", unresolved === 0);
  });
}

/** initSpeakerMaps 初始化本场说话人批量对应。 */
function initSpeakerMaps() {
  document.querySelectorAll("[data-speaker-map]").forEach((select) => {
    select.dataset.savedValue = select.value;
    select.addEventListener("change", () => updateClusterPreview(select));
  });
}

/** initSegmentSpeakers 初始化每条记录直接可选的说话人下拉框。 */
function initSegmentSpeakers() {
  document.querySelectorAll("[data-segment-speaker]").forEach((select) => {
    select.dataset.savedValue = select.value;
    select.addEventListener("change", () => {
      const item = select.closest("[data-transcript]");
      const clusterSelect = document.querySelector(`[data-speaker-map="${item?.dataset.cluster}"]`);
      const usesCluster = select.value === clusterSelect?.value;
      select.dataset.userOverride = usesCluster ? "false" : "true";
      if (select.value === select.dataset.savedValue) dirtySpeakerOverrides.delete(select);
      else dirtySpeakerOverrides.add(select);
      if (item) updateTranscriptDirtyState(item);
      updateSaveSummary();
    });
  });
}

/** initPlayback 初始化同一时刻只播放一个片段的原型反馈。 */
function initPlayback() {
  document.querySelectorAll("[data-play]").forEach((button) => {
    button.addEventListener("click", () => {
      if (playingButton && playingButton !== button) playingButton.textContent = "播放";
      const willPlay = button.textContent.trim() === "播放";
      button.textContent = willPlay ? "暂停" : "播放";
      playingButton = willPlay ? button : null;
    });
  });
}

/** completeSave 模拟保存后的页面状态，用于审核交互反馈。 */
function completeSave() {
  const dirtyItems = new Set();
  dirtyTextInputs.forEach((input) => {
    input.dataset.savedValue = input.value;
    dirtyItems.add(input.closest("[data-transcript]"));
  });
  dirtySpeakerMaps.forEach((select) => (select.dataset.savedValue = select.value));
  dirtySpeakerOverrides.forEach((select) => {
    select.dataset.savedValue = select.value;
    dirtyItems.add(select.closest("[data-transcript]"));
  });
  dirtyItems.forEach((item) => {
    const status = item?.querySelector(".status");
    if (status) {
      status.textContent = "已人工修改";
      status.className = "status info";
    }
  });

  dirtyTextInputs.clear();
  dirtySpeakerMaps.clear();
  dirtySpeakerOverrides.clear();
  document.querySelectorAll("[data-transcript]").forEach(updateTranscriptDirtyState);
  const fileStatus = document.querySelector("[data-file-status]");
  if (fileStatus) fileStatus.textContent = "原始记录已刷新";
  updateSaveSummary();
  showToast("原型演示：修改已保存");
}

/** saveAll 模拟标题行中的页面级保存。 */
function saveAll(callback) {
  const save = document.querySelector("[data-save-all]");
  if (!save || getDirtyCount() === 0) {
    callback?.();
    return;
  }
  save.disabled = true;
  save.textContent = "正在保存";
  window.setTimeout(() => {
    completeSave();
    save.textContent = "保存修改";
    callback?.();
  }, 450);
}

/** initSaveActions 初始化保存修改和保存后返回。 */
function initSaveActions() {
  document.querySelector("[data-save-all]")?.addEventListener("click", () => saveAll());
  document.querySelector("[data-save-and-leave]")?.addEventListener("click", () => {
    saveAll(() => {
      window.location.href = "meeting-detail.html";
    });
  });
}

/** initLeaveGuard 在存在未保存内容时展示离开确认。 */
function initLeaveGuard() {
  document.querySelector("[data-leave-link]")?.addEventListener("click", (event) => {
    if (getDirtyCount() === 0) return;
    event.preventDefault();
    openModal("unsaved", event.currentTarget);
  });
}

/** initModals 初始化弹窗的关闭和 Escape 行为。 */
function initModals() {
  document.querySelectorAll(".modal-backdrop").forEach((modal) => {
    modal.querySelectorAll("[data-close-modal]").forEach((button) => {
      button.addEventListener("click", () => closeModal(modal));
    });
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return;
    const modal = document.querySelector(".modal-backdrop.open");
    if (modal) closeModal(modal);
  });
}

/** initProposal 启动页面中的全部静态原型交互。 */
function initProposal() {
  initPlayback();
  initSpeakerMaps();
  initSegmentSpeakers();
  initTranscriptInputs();
  initSaveActions();
  initLeaveGuard();
  initModals();
  updateSpeakerStatus();
  updateSaveSummary();
}

document.addEventListener("DOMContentLoaded", initProposal);
