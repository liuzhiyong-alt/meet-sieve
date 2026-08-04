/** showToast 展示短暂的原型反馈，不代表真实业务操作完成。 */
function showToast(message) {
  let toast = document.querySelector(".toast");
  if (!toast) {
    toast = document.createElement("div");
    toast.className = "toast";
    toast.setAttribute("aria-live", "polite");
    document.body.appendChild(toast);
  }
  toast.textContent = message;
  toast.classList.add("show");
  window.clearTimeout(showToast.timer);
  showToast.timer = window.setTimeout(
    () => toast.classList.remove("show"),
    2200,
  );
}

/** activateTab 切换同一组 Tab，并同步面板显隐。 */
function activateTab(tab) {
  const group = tab.closest("[data-tabs]");
  if (!group) return;
  const name = tab.dataset.tab;
  group.querySelectorAll("[data-tab]").forEach((item) => {
    const active = item === tab;
    item.classList.toggle("active", active);
    item.setAttribute("aria-selected", String(active));
  });
  document
    .querySelectorAll(`[data-tab-panel="${group.dataset.tabs}"]`)
    .forEach((panel) => {
      panel.hidden = panel.id !== name;
    });
  const url = new URL(window.location.href);
  if (group.dataset.tabs === "people") {
    url.searchParams.set("tab", name);
    window.history.replaceState({}, "", url);
  }
  if (group.dataset.tabs === "settings") {
    const breadcrumb = document.getElementById("settings-breadcrumb-current");
    if (breadcrumb) breadcrumb.textContent = tab.textContent.trim();
  }
}

/** initTabs 初始化页面中的设置分类和成员库 Tab。 */
function initTabs() {
  document.querySelectorAll("[data-tabs] [data-tab]").forEach((tab) => {
    tab.addEventListener("click", () => activateTab(tab));
  });
  const requested = new URL(window.location.href).searchParams.get("tab");
  if (requested) {
    const tab = document.querySelector(`[data-tabs] [data-tab="${requested}"]`);
    if (tab) activateTab(tab);
  }
}

let modalTrigger;

/** openModal 打开短任务弹窗并把焦点移入表单。 */
function openModal(id, trigger) {
  const backdrop = document.getElementById(id);
  if (!backdrop) return;
  modalTrigger = trigger;
  backdrop.classList.add("open");
  backdrop.querySelector("input, button, select")?.focus();
}

/** closeModal 关闭弹窗并恢复触发控件焦点。 */
function closeModal(backdrop) {
  backdrop.classList.remove("open");
  modalTrigger?.focus();
}

/** initModals 初始化弹窗打开、关闭、遮罩和 Escape 行为。 */
function initModals() {
  document.querySelectorAll("[data-open]").forEach((trigger) => {
    trigger.addEventListener("click", () => {
      const memberName =
        trigger.closest("[data-member-row]")?.dataset.memberName;
      const target = document.querySelector("[data-delete-member-name]");
      if (memberName && target) target.textContent = memberName;
      document.body.dataset.deleteMember =
        trigger.closest("[data-member-row]")?.id || "";
      openModal(trigger.dataset.open, trigger);
    });
  });
  document.querySelectorAll(".modal-backdrop").forEach((backdrop) => {
    backdrop.addEventListener("click", (event) => {
      if (event.target === backdrop) closeModal(backdrop);
    });
    backdrop.querySelectorAll("[data-close]").forEach((button) => {
      button.addEventListener("click", () => closeModal(backdrop));
    });
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return;
    const opened = document.querySelector(".modal-backdrop.open");
    if (opened) closeModal(opened);
  });
}

/** initAdvanced 控制创建会议页高级设置展开状态。 */
function initAdvanced() {
  const trigger = document.querySelector("[data-advanced-toggle]");
  const panel = document.querySelector("[data-advanced-panel]");
  if (!trigger || !panel) return;
  if (new URL(window.location.href).searchParams.get("advanced") === "1") {
    panel.hidden = false;
    trigger.setAttribute("aria-expanded", "true");
    trigger.textContent = "收起高级设置";
  }
  trigger.addEventListener("click", () => {
    panel.hidden = !panel.hidden;
    trigger.setAttribute("aria-expanded", String(!panel.hidden));
    trigger.textContent = panel.hidden ? "展开高级设置" : "收起高级设置";
  });
}

/** initToggles 切换原型中的二态设置，并显示关联的附加内容。 */
function initToggles() {
  document.querySelectorAll("[data-toggle]").forEach((toggle) => {
    toggle.addEventListener("click", () => {
      const enabled = !toggle.classList.contains("on");
      toggle.classList.toggle("on", enabled);
      toggle.setAttribute("aria-pressed", String(enabled));
      const selector = toggle.dataset.controls;
      const detail = selector ? document.querySelector(selector) : null;
      if (detail) detail.hidden = !enabled;
    });
  });
}

/** appendTemporaryParticipant 把临时成员加入会前选择区。 */
function appendTemporaryParticipant(name) {
  const list = document.querySelector("[data-temporary-list]");
  if (!list) return;
  const item = document.createElement("li");
  const label = document.createElement("span");
  const remove = document.createElement("button");
  label.textContent = `${name}（临时）`;
  remove.type = "button";
  remove.textContent = "移除";
  remove.setAttribute("aria-label", `移除临时成员 ${name}`);
  remove.addEventListener("click", () => item.remove());
  item.append(label, remove);
  list.appendChild(item);
}

/** initTemporaryParticipant 初始化临时成员弹窗提交。 */
function initTemporaryParticipant() {
  const form = document.querySelector("[data-temporary-form]");
  if (!form) return;
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    const input = form.querySelector("input");
    const name = input.value.trim();
    if (!name) return;
    appendTemporaryParticipant(name);
    input.value = "";
    closeModal(form.closest(".modal-backdrop"));
    showToast(`已添加临时成员：${name}`);
  });
}

/** initGroupPrefill 用假数据演示小组选择后参会人批量勾选。 */
function initGroupPrefill() {
  const select = document.querySelector("[data-group-select]");
  if (!select) return;
  select.addEventListener("change", () => {
    const selected = new Set(
      (select.selectedOptions[0]?.dataset.members || "").split(","),
    );
    document.querySelectorAll("[data-member-choice]").forEach((choice) => {
      choice.checked = selected.has(choice.value);
    });
  });
}

/** initMemberDelete 用假数据演示成员列表统一删除入口。 */
function initMemberDelete() {
  const confirm = document.querySelector("[data-confirm-delete-member]");
  if (!confirm) return;
  confirm.addEventListener("click", () => {
    const row = document.getElementById(
      document.body.dataset.deleteMember || "",
    );
    const name = row?.dataset.memberName || "该成员";
    row?.classList.add("is-removed");
    closeModal(confirm.closest(".modal-backdrop"));
    showToast(`${name} 已从成员库删除，历史会议快照仍保留`);
  });
}

/** initRecords 初始化假数据筛选和两页分页交互。 */
function initRecords() {
  const form = document.querySelector("[data-records-form]");
  if (!form) return;
  const rows = [...document.querySelectorAll("[data-record]")];
  const previous = document.querySelector("[data-page-previous]");
  const next = document.querySelector("[data-page-next]");
  const pageLabel = document.querySelector("[data-page-label]");
  let page =
    new URL(window.location.href).searchParams.get("page") === "2" ? 2 : 1;

  /** syncRecordPage 把原型页码写入 URL，便于刷新后继续检查当前页面。 */
  function syncRecordPage() {
    const url = new URL(window.location.href);
    if (page === 1) url.searchParams.delete("page");
    else url.searchParams.set("page", String(page));
    window.history.replaceState({}, "", url);
  }

  /** renderRecords 按当前筛选和页码更新假数据列表。 */
  function renderRecords() {
    const search = form.elements.search.value.trim().toLowerCase();
    const status = form.elements.status.value;
    const matched = rows.filter((row) => {
      return (
        (!search || row.dataset.search.toLowerCase().includes(search)) &&
        (!status || row.dataset.status === status)
      );
    });
    const pageSize = 10;
    const maxPage = Math.max(1, Math.ceil(matched.length / pageSize));
    page = Math.min(page, maxPage);
    rows.forEach((row) => (row.hidden = true));
    matched
      .slice((page - 1) * pageSize, page * pageSize)
      .forEach((row) => (row.hidden = false));
    previous.disabled = page === 1;
    next.disabled = page === maxPage;
    pageLabel.textContent = `第 ${page} 页 · 当前 ${Math.min(pageSize, Math.max(0, matched.length - (page - 1) * pageSize))} 场`;
    syncRecordPage();
  }

  form.addEventListener("submit", (event) => {
    event.preventDefault();
    page = 1;
    renderRecords();
  });
  previous.addEventListener("click", () => {
    page -= 1;
    renderRecords();
  });
  next.addEventListener("click", () => {
    page += 1;
    renderRecords();
  });
  renderRecords();
}

/** initPrototypeFeedback 初始化保存、编辑等不改变真实数据的演示反馈。 */
function initPrototypeFeedback() {
  document.querySelectorAll("[data-prototype-feedback]").forEach((button) => {
    button.addEventListener("click", () => {
      const statusSelector = button.dataset.statusTarget;
      const status = statusSelector
        ? document.querySelector(statusSelector)
        : null;
      if (status) {
        status.textContent = button.dataset.statusText || "已保存";
        status.classList.add("ok");
      }
      showToast(button.dataset.prototypeFeedback);
    });
  });
}

document.addEventListener("DOMContentLoaded", () => {
  initTabs();
  initModals();
  initAdvanced();
  initToggles();
  initTemporaryParticipant();
  initGroupPrefill();
  initMemberDelete();
  initRecords();
  initPrototypeFeedback();
});
