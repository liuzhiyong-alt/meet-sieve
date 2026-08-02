/* MeetSieve 原型交互：标签、弹窗、开关与流程状态。 */
document.addEventListener("DOMContentLoaded", () => {
  bindTabs();
  bindModals();
  bindToggles();
  bindAdvancedSettings();
  bindMeetingFlow();
  bindCopyActions();
  bindActionFeedback();
});

/**
 * 激活指定标签，并同步标签与面板的可访问状态。
 * @param {HTMLElement} root 标签容器。
 * @param {HTMLElement} button 需要激活的标签按钮。
 */
function activateTab(root, button) {
  const target = button.dataset.tab;
  root.querySelectorAll("[data-tab]").forEach((item) => {
    const isActive = item === button;
    item.classList.toggle("active", isActive);
    item.setAttribute("aria-selected", String(isActive));
    item.tabIndex = isActive ? 0 : -1;
  });
  document.querySelectorAll(`[data-tab-panel="${root.dataset.tabs}"]`).forEach((panel) => {
    panel.hidden = panel.id !== target;
  });
}

/**
 * 绑定页内标签切换，并支持方向键导航。
 */
function bindTabs() {
  document.querySelectorAll("[data-tabs]").forEach((root) => {
    root.setAttribute("role", "tablist");
    const tabs = [...root.querySelectorAll("[data-tab]")];
    tabs.forEach((button, index) => {
      button.setAttribute("role", "tab");
      button.addEventListener("click", () => activateTab(root, button));
      button.addEventListener("keydown", (event) => {
        if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
        event.preventDefault();
        const nextIndex = event.key === "Home"
          ? 0
          : event.key === "End"
            ? tabs.length - 1
            : (index + (event.key === "ArrowRight" ? 1 : -1) + tabs.length) % tabs.length;
        tabs[nextIndex].focus();
        activateTab(root, tabs[nextIndex]);
      });
    });
    const hashTarget = window.location.hash.slice(1);
    const initialTab = tabs.find((button) => button.dataset.tab === hashTarget)
      || root.querySelector(".active")
      || tabs[0];
    activateTab(root, initialTab);
  });
}

/**
 * 关闭弹窗并将焦点还给触发按钮。
 * @param {HTMLElement} backdrop 弹窗遮罩。
 */
function closeModal(backdrop) {
  backdrop.classList.remove("open");
  backdrop._trigger?.focus();
}

/**
 * 绑定通用弹窗打开、焦点管理与关闭行为。
 */
function bindModals() {
  document.querySelectorAll("[data-open]").forEach((button) => {
    button.addEventListener("click", () => {
      const backdrop = document.getElementById(button.dataset.open);
      if (!backdrop) return;
      const modal = backdrop.querySelector(".modal");
      const title = modal?.querySelector("h2");
      backdrop._trigger = button;
      backdrop.classList.add("open");
      modal?.setAttribute("role", "dialog");
      modal?.setAttribute("aria-modal", "true");
      if (title) {
        title.id ||= `${backdrop.id}-title`;
        modal.setAttribute("aria-labelledby", title.id);
      }
      modal?.querySelector("input, textarea, select, button, a[href]")?.focus();
    });
  });
  document.querySelectorAll("[data-close]").forEach((button) => {
    button.addEventListener("click", () => {
      const backdrop = button.closest(".modal-backdrop");
      if (backdrop) closeModal(backdrop);
    });
  });
  document.querySelectorAll(".modal-backdrop").forEach((backdrop) => {
    backdrop.addEventListener("click", (event) => {
      if (event.target === backdrop) closeModal(backdrop);
    });
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return;
    const backdrop = document.querySelector(".modal-backdrop.open");
    if (backdrop) closeModal(backdrop);
  });
}

/**
 * 绑定二态开关，并同步可访问状态。
 */
function bindToggles() {
  document.querySelectorAll(".toggle").forEach((toggle) => {
    toggle.addEventListener("click", () => {
      const enabled = toggle.classList.toggle("on");
      toggle.setAttribute("aria-pressed", String(enabled));
    });
  });
}

/**
 * 控制创建会议页的高级设置折叠区。
 */
function bindAdvancedSettings() {
  const trigger = document.querySelector("[data-advanced-toggle]");
  const panel = document.querySelector("[data-advanced-panel]");
  if (!trigger || !panel) return;
  trigger.addEventListener("click", () => {
    panel.hidden = !panel.hidden;
    trigger.textContent = panel.hidden ? "展开高级设置" : "收起高级设置";
  });
}

/**
 * 绑定会议原型中的 AI 提问、结束会议和空状态选择清理。
 */
function bindMeetingFlow() {
  const askForm = document.querySelector("[data-ai-form]");
  if (askForm) {
    askForm.addEventListener("submit", (event) => {
      event.preventDefault();
      const input = askForm.querySelector("textarea");
      const output = document.querySelector("[data-ai-output]");
      if (!input.value.trim()) {
        input.focus();
        input.setAttribute("aria-invalid", "true");
        showFieldError(input, "请输入问题后再提交。");
        return;
      }
      input.removeAttribute("aria-invalid");
      input.nextElementSibling?.classList.contains("field-error") && input.nextElementSibling.remove();
      output.hidden = false;
      output.querySelector("[data-question]").textContent = input.value.trim();
      input.value = "";
      const backdrop = askForm.closest(".modal-backdrop");
      if (backdrop) closeModal(backdrop);
      showToast("问题已写入时间线，AI 正在参与。");
    });
  }

  document.querySelectorAll("[data-empty-state]").forEach((button) => {
    button.addEventListener("click", () => {
      document.querySelectorAll("[data-bulk-select]").forEach((checkbox) => {
        checkbox.checked = false;
      });
      const list = document.querySelector("[data-record-list]");
      if (list) list.innerHTML = '<div class="empty"><h3>没有符合条件的会议</h3><p>已清空批量选择。调整筛选条件后再试。</p></div>';
    });
  });
}

/**
 * 在字段下方显示可访问的校验提示。
 * @param {HTMLElement} input 需要提示的表单字段。
 * @param {string} message 提示文案。
 */
function showFieldError(input, message) {
  let error = input.nextElementSibling;
  if (!error?.classList.contains("field-error")) {
    error = document.createElement("p");
    error.className = "field-error";
    error.setAttribute("role", "alert");
    input.insertAdjacentElement("afterend", error);
  }
  error.textContent = message;
}

/**
 * 显示短暂的全局操作反馈。
 * @param {string} message 反馈文案。
 */
function showToast(message) {
  let toast = document.querySelector("[data-toast]");
  if (!toast) {
    toast = document.createElement("div");
    toast.className = "toast";
    toast.dataset.toast = "";
    toast.setAttribute("role", "status");
    toast.setAttribute("aria-live", "polite");
    document.body.append(toast);
  }
  toast.textContent = message;
  toast.classList.add("show");
  window.clearTimeout(toast._timer);
  toast._timer = window.setTimeout(() => toast.classList.remove("show"), 2200);
}

/**
 * 为需要等待的原型动作补充加载、完成与弹窗关闭反馈。
 */
function bindActionFeedback() {
  document.querySelectorAll("[data-action-feedback]").forEach((button) => {
    button.addEventListener("click", () => {
      if (button.getAttribute("aria-busy") === "true") return;
      const message = button.dataset.actionFeedback;
      button.setAttribute("aria-busy", "true");
      button.disabled = true;
      window.setTimeout(() => {
        button.removeAttribute("aria-busy");
        button.disabled = false;
        const backdrop = button.closest(".modal-backdrop");
        if (backdrop) closeModal(backdrop);
        showToast(message);
      }, 800);
    });
  });
}

/**
 * 绑定复制命令操作，并提供短暂反馈。
 */
function bindCopyActions() {
  document.querySelectorAll("[data-copy]").forEach((button) => {
    button.addEventListener("click", async () => {
      const original = button.textContent;
      try {
        await navigator.clipboard.writeText(button.dataset.copy);
        button.textContent = "已复制";
        showToast("已复制到剪贴板。");
      } catch {
        button.textContent = "请手动复制";
        showToast("无法访问剪贴板，请手动复制。");
      }
      window.setTimeout(() => {
        button.textContent = original;
      }, 1600);
    });
  });
}
