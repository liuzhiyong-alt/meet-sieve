import { reactive } from 'vue'

export interface BreadcrumbDefinition {
  label?: string
  dynamic?: 'current' | 'meeting'
  to?: string
}

interface DynamicBreadcrumbTitles {
  current?: string
  meeting?: string
}

const titlesByPath = reactive<Record<string, DynamicBreadcrumbTitles>>({})

/** setBreadcrumbTitles 保存页面从真实投影加载出的安全名称，不把 UUID 当作标题。 */
export function setBreadcrumbTitles(
  path: string,
  titles: DynamicBreadcrumbTitles,
): void {
  titlesByPath[path] = { ...titles }
}

/** getBreadcrumbTitles 返回指定路由当前已加载的动态面包屑名称。 */
export function getBreadcrumbTitles(path: string): DynamicBreadcrumbTitles {
  return titlesByPath[path] ?? {}
}
