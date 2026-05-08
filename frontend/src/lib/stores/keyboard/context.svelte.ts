import {
  getRoute,
  getPage,
  getDetailTab,
  isDiffView,
} from "../router.svelte.js";
import type { Context } from "./types.js";
import type { PullSelection } from "@middleman/ui/stores/pulls";
import type { IssueSelection } from "@middleman/ui/stores/issues";

interface SelectionSources {
  pulls: { getSelectedPR: () => PullSelection | null };
  issues: { getSelectedIssue: () => IssueSelection | null };
}

export function buildContext(stores: SelectionSources): Context {
  return {
    page: getPage(),
    route: getRoute(),
    selectedPR: stores.pulls.getSelectedPR(),
    selectedIssue: stores.issues.getSelectedIssue(),
    isDiffView: isDiffView(),
    detailTab: getDetailTab(),
  };
}
