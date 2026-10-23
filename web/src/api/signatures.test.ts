import {expect,it} from 'vitest'
import {withSignature} from './signatures'
it('adds the chosen signature to both alternatives and escapes its HTML',()=>{
 const result=withSignature('Hello','<p>Hello</p>','Team <support>\nLine two')
 expect(result.text).toBe('Hello\n\n-- \nTeam <support>\nLine two')
 expect(result.html).toBe('<p>Hello</p><p>-- <br>Team &lt;support&gt;<br>Line two</p>')
 expect(withSignature('Hello','','')).toEqual({text:'Hello',html:''})
})
