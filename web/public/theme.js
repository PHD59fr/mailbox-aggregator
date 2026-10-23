// The authenticated profile supplies its palette after vault unlock.
globalThis.document.documentElement.dataset.theme='slate'
try{globalThis.localStorage.removeItem('mailbox-theme')}catch{/* Obsolete device-wide preference. */}
