/*
 * 会中时间线设计原型交互。
 * 文件只读取本地文件名和大小来演示发送状态，不会上传或读取文件内容。
 */

const latestThreshold = 48;
const timelineState = {
  followLatest: true,
  programmaticScroll: false,
  unreadCount: 0,
  hasUnreadPartial: false,
};

/** 获取时间线滚动容器。 */
function getTimeline() {
  return document.querySelector(".conversation");
}

/** 判断用户是否位于最新消息附近。 */
function isNearLatest() {
  const timeline = getTimeline();
  if (!timeline) return true;
  return (
    timeline.scrollHeight - timeline.scrollTop - timeline.clientHeight <=
    latestThreshold
  );
}

/** 清除新消息提示并恢复跟随最新状态。 */
function clearNewMessageIndicator() {
  const indicator = document.querySelector("[data-new-message-indicator]");
  if (indicator) indicator.hidden = true;
  timelineState.unreadCount = 0;
  timelineState.hasUnreadPartial = false;
}

/** 更新查看历史时显示的新消息提示。 */
function updateNewMessageIndicator(kind) {
  const indicator = document.querySelector("[data-new-message-indicator]");
  const label = document.querySelector("[data-new-message-text]");
  if (!indicator || !label) return;

  if (kind === "partial") timelineState.hasUnreadPartial = true;
  if (kind === "event") timelineState.unreadCount += 1;
  label.textContent = timelineState.unreadCount
    ? `${timelineState.unreadCount} 条新消息`
    : "有新的实时转写";
  indicator.hidden = false;
}

/** 滚动到最新消息，并在滚动结束后重新启用用户位置判断。 */
function resumeFollowingLatest(behavior = "smooth") {
  const timeline = getTimeline();
  if (!timeline) return;

  timelineState.followLatest = true;
  timelineState.programmaticScroll = true;
  clearNewMessageIndicator();
  const reduceMotion = window.matchMedia(
    "(prefers-reduced-motion: reduce)",
  ).matches;
  timeline.scrollTo({
    top: timeline.scrollHeight,
    behavior: reduceMotion ? "auto" : behavior,
  });
  window.setTimeout(
    () => {
      timelineState.programmaticScroll = false;
      timelineState.followLatest = isNearLatest();
    },
    behavior === "smooth" && !reduceMotion ? 280 : 0,
  );
}

/** 在时间线内容变高后决定跟随底部或只显示新消息提示。 */
function handleTimelineGrowth(kind) {
  if (timelineState.followLatest) {
    window.requestAnimationFrame(() => resumeFollowingLatest("auto"));
    return;
  }
  updateNewMessageIndicator(kind);
}

/** 监听用户滚动，离开底部后暂停自动跟随。 */
function bindTimelineFollowing() {
  const timeline = getTimeline();
  const indicator = document.querySelector("[data-new-message-indicator]");
  if (!timeline || !indicator) return;

  timeline.addEventListener("scroll", () => {
    if (timelineState.programmaticScroll) return;
    timelineState.followLatest = isNearLatest();
    if (timelineState.followLatest) clearNewMessageIndicator();
  });
  indicator.addEventListener("click", () => resumeFollowingLatest());
}

/** 自动调整消息输入框高度，最多展示四行。 */
function resizeComposerInput(input) {
  input.style.height = "auto";
  input.style.height = `${Math.min(input.scrollHeight, 96)}px`;
  input.style.overflowY = input.scrollHeight > 96 ? "auto" : "hidden";
}

/** 创建一条通用时间线事件骨架。 */
function createTimelineItem({ avatar, name, kind, className = "" }) {
  const item = document.createElement("li");
  item.className = `conversation-item ${className}`.trim();

  const avatarElement = document.createElement("span");
  avatarElement.className = "avatar speaker-avatar";
  avatarElement.setAttribute("aria-hidden", "true");
  avatarElement.textContent = avatar;

  const body = document.createElement("article");
  body.className = "conversation-body";
  const header = document.createElement("header");
  header.className = "message-head";
  const speaker = document.createElement("strong");
  speaker.textContent = name;
  const eventKind = document.createElement("span");
  eventKind.className = "event-kind";
  eventKind.textContent = kind;
  const time = document.createElement("time");
  time.className = "meta num";
  time.textContent = "现在";
  header.append(speaker, eventKind, time);
  body.append(header);
  item.append(avatarElement, body);
  return { item, body };
}

/** 把主持人文字作为正式会议消息追加到时间线。 */
function appendHostMessage(text) {
  const timeline = getTimeline();
  if (!timeline) return;
  const { item, body } = createTimelineItem({
    avatar: "你",
    name: "你",
    kind: "主持人消息",
    className: "is-host-message",
  });
  const content = document.createElement("p");
  content.textContent = text;
  body.append(content);
  timeline.append(item);
  resumeFollowingLatest();
}

/** 把文件大小格式化为适合附件消息展示的文本。 */
function formatFileSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

/** 获取文件扩展名，无法识别时使用通用 FILE 标签。 */
function fileTypeLabel(fileName) {
  const extension = fileName.includes(".") ? fileName.split(".").pop() : "";
  return (extension || "FILE").slice(0, 5).toUpperCase();
}

/** 选择文件确认后立即创建附件消息，并模拟本地发送状态。 */
function appendAttachment(file) {
  const timeline = getTimeline();
  if (!timeline) return;
  const { item, body } = createTimelineItem({
    avatar: "你",
    name: "你",
    kind: "附件",
    className: "is-resource",
  });
  const resource = document.createElement("button");
  resource.className = "resource-row";
  resource.type = "button";
  resource.disabled = true;
  const type = document.createElement("span");
  type.className = "resource-type";
  type.setAttribute("aria-hidden", "true");
  type.textContent = fileTypeLabel(file.name);
  const copy = document.createElement("span");
  copy.className = "resource-copy";
  const name = document.createElement("strong");
  name.textContent = file.name;
  const state = document.createElement("span");
  state.textContent = `${formatFileSize(file.size)} · 正在发送`;
  copy.append(name, state);
  const action = document.createElement("span");
  action.className = "resource-action";
  action.textContent = "发送中";
  resource.append(type, copy, action);
  body.append(resource);
  timeline.append(item);
  resumeFollowingLatest();

  window.setTimeout(() => {
    state.textContent = `${formatFileSize(file.size)} · 已进入本场资料`;
    action.textContent = "打开";
    resource.disabled = false;
  }, 900);
}

/** 绑定文本发送、快捷键和输入框自适应高度。 */
function bindComposer() {
  const form = document.querySelector(".meeting-composer");
  const input = document.querySelector("#meeting-message");
  if (!form || !(input instanceof HTMLTextAreaElement)) return;

  resizeComposerInput(input);
  input.addEventListener("input", () => resizeComposerInput(input));
  input.addEventListener("keydown", (event) => {
    if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      form.requestSubmit();
    }
  });
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    const text = input.value.trim();
    if (!text) {
      input.focus();
      return;
    }
    appendHostMessage(text);
    input.value = "";
    resizeComposerInput(input);
  });
}

/** 绑定附件图标和系统文件选择窗口。 */
function bindAttachmentPicker() {
  const trigger = document.querySelector("[data-attachment-trigger]");
  const picker = document.querySelector("[data-file-picker]");
  if (!trigger || !(picker instanceof HTMLInputElement)) return;

  trigger.addEventListener("click", () => picker.click());
  picker.addEventListener("change", () => {
    const file = picker.files?.[0];
    picker.value = "";
    if (file) appendAttachment(file);
  });
}

/** 使用逐字补充模拟 ASR partial，同一条消息不会重复新增。 */
function startPartialTyping() {
  const target = document.querySelector("[data-partial-text]");
  if (!target) return;
  const text =
    "输入区已经压缩，新的消息会在底部持续补充，查看历史时不会抢走当前位置。";
  if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
    target.textContent = text;
    handleTimelineGrowth("partial");
    return;
  }

  let index = target.textContent.length;
  window.setTimeout(() => {
    const timer = window.setInterval(() => {
      index += 1;
      target.textContent = text.slice(0, index);
      handleTimelineGrowth("partial");
      if (index >= text.length) window.clearInterval(timer);
    }, 42);
  }, 700);
}

/** 初始化设计预览中的会中交互。 */
function initializeLiveTimelineDemo() {
  bindTimelineFollowing();
  bindComposer();
  bindAttachmentPicker();
  resumeFollowingLatest("auto");
  startPartialTyping();
}

document.addEventListener("DOMContentLoaded", initializeLiveTimelineDemo);
