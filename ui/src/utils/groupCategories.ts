import type { Group } from '../types';

export const DEFAULT_CATEGORY = 'Global';

export interface CategorySection<T extends Group> {
  category: string;
  groups: T[];
}

/**
 * Buckets groups by category, sorted alphabetically with the default
 * "Global" category last.
 */
export function groupByCategory<T extends Group>(groups: T[]): CategorySection<T>[] {
  const buckets = new Map<string, T[]>();

  for (const group of groups) {
    const category = group.category || DEFAULT_CATEGORY;
    const bucket = buckets.get(category);
    if (bucket) {
      bucket.push(group);
    } else {
      buckets.set(category, [group]);
    }
  }

  return [...buckets.entries()]
    .sort(([a], [b]) => {
      if (a === DEFAULT_CATEGORY) return 1;
      if (b === DEFAULT_CATEGORY) return -1;
      return a.localeCompare(b);
    })
    .map(([category, categoryGroups]) => ({ category, groups: categoryGroups }));
}

/**
 * Category headers are only worth showing when groups actually use
 * categories; a lone "Global" section renders as a plain list.
 */
export function shouldShowCategories<T extends Group>(sections: CategorySection<T>[]): boolean {
  return sections.length > 1 || (sections.length === 1 && sections[0].category !== DEFAULT_CATEGORY);
}
