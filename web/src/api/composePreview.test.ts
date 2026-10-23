import {describe,expect,it} from 'vitest'
import {composePreviewDocument} from './composePreview'

describe('local compose preview',()=>{
  it('removes active content, remote resources and attacker attributes while retaining formatted text',()=>{
    const html=composePreviewDocument('<script>alert(1)</script><img src="https://tracker.test"><base href="https://evil.test"><form><input></form><iframe src="https://evil.test"></iframe><p onclick="alert(1)" style="background:url(https://tracker.test)">Hello <strong>team</strong></p><p style="text-align:center">Aligned</p><a href="javascript:alert(1)">Link</a>')
    const document=new DOMParser().parseFromString(html,'text/html')
    expect(document.body.innerHTML).toContain('<strong>team</strong>')
    expect(document.body.querySelector('script,img,form,input,iframe,base,[onclick],[style],[href]')).toBeNull()
    expect(document.querySelector('p.email-align-center')?.textContent).toBe('Aligned')
    expect(html).not.toContain('tracker.test');expect(html).not.toContain('evil.test')
  })
})
