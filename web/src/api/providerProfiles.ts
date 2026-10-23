export const providerProfiles = {
  gmail:{name:'Gmail / Google Workspace',imap:{host:'imap.gmail.com',port:993,security:'tls'},smtp:{host:'smtp.gmail.com',port:465,security:'tls'},note:'Use OAuth when configured, or an app password if your Google account permits it.'},
  outlook:{name:'Outlook.com',imap:{host:'outlook.office365.com',port:993,security:'tls'},smtp:{host:'smtp-mail.outlook.com',port:587,security:'starttls'},note:'Enable IMAP in Outlook settings. Modern authentication (OAuth) is required.'},
  microsoft365:{name:'Microsoft 365',imap:{host:'outlook.office365.com',port:993,security:'tls'},smtp:{host:'smtp.office365.com',port:587,security:'starttls'},note:'Your administrator must allow IMAP and authenticated SMTP. OAuth is normally required.'},
} as const
export type ProviderProfile = keyof typeof providerProfiles
export function suggestedProvider(email:string):ProviderProfile|''{
  const domain=email.trim().toLowerCase().split('@')[1]
  if(domain==='gmail.com'||domain==='googlemail.com')return 'gmail'
  if(['outlook.com','outlook.fr','hotmail.com','hotmail.fr','live.com','live.fr','msn.com'].includes(domain||''))return 'outlook'
  return ''
}
