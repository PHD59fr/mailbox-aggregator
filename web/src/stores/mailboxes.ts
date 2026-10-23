import { defineStore } from 'pinia'
import type { MailboxStatus } from '../api/mailboxes'
import type { Account } from '../api/accounts'
import {defaultShortcuts,type WorkspaceDisplay} from '../api/workspaceDisplay'
import type { CombinedView, MailSource } from '../api/mailSources'

const intervals = [15, 30, 60, 120, 300]
function savedInterval() {
  try { const value = Number(localStorage.getItem('mailbox-refresh-seconds')); return intervals.includes(value) ? value : 30 } catch { return 30 }
}
export const useMailboxStore = defineStore('mailboxes', {
  state: () => ({ accounts: [] as Account[], statuses: {} as Record<string, MailboxStatus>, error: '', monitoring: false, refreshInterval: savedInterval(), refreshToken: 0,
    search: '', advancedSearch: '', readFilter: '' as '' | 'read' | 'unread', senderFilter: '', recipientFilter:'',subjectFilter:'',sinceFilter:'',untilFilter:'', filtersOpen: false,
    display: {...defaultShortcuts(),id:'workspace-display',density:'comfortable',layout:'split',colors:{}} as WorkspaceDisplay,
    view: 'inbox' as CombinedView, searchScope: 'current' as 'current' | 'all', selectedFolders: [] as MailSource[], starredFilter: false,tagFilter:'' }),
  getters: {
    unseen: state => Object.values(state.statuses).reduce((sum, status) => sum + status.unseen, 0),
    messages: state => Object.values(state.statuses).reduce((sum, status) => sum + status.messages, 0),
    filtering: state => !!(state.search.trim() || state.advancedSearch.trim() || state.readFilter || state.senderFilter.trim() || state.recipientFilter.trim() || state.subjectFilter.trim() || state.sinceFilter || state.untilFilter || state.starredFilter),
    filterCount:state=>[state.readFilter,state.senderFilter.trim(),state.recipientFilter.trim(),state.subjectFilter.trim(),state.sinceFilter,state.untilFilter].filter(Boolean).length,
  },
  actions: {
    setInterval(value: number) {
      if (!intervals.includes(value)) return
      this.refreshInterval = value
      try { localStorage.setItem('mailbox-refresh-seconds', String(value)) } catch { /* Browser-only, non-secret preference. */ }
    },
    refresh() { this.refreshToken++ },
    clearFilters() { this.search = ''; this.advancedSearch = ''; this.readFilter = ''; this.senderFilter = '';this.recipientFilter='';this.subjectFilter='';this.sinceFilter='';this.untilFilter='';this.starredFilter=false },
    clear() { this.accounts = []; this.statuses = {}; this.error = ''; this.monitoring = false; this.clearFilters(); this.filtersOpen = false;this.tagFilter='';this.view='inbox';this.searchScope='current';this.selectedFolders=[];this.display={...defaultShortcuts(),id:'workspace-display',density:'comfortable',layout:'split',colors:{}} },
  },
})
