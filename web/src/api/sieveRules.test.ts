import {describe,it,expect} from 'vitest'
import {generateSieve,newSieveRule,simulateSieve} from './sieveRules'
describe('bounded Sieve rules',()=>{
 it('quotes values, requires extensions and preserves rule priority',()=>{
  const rule={...newSieveRule(),conditions:[{field:'subject' as const,match:'contains' as const,value:'a"b\\c'}],action:'fileinto' as const,destination:'Screen Emails'}
  expect(()=>generateSieve([rule],[])).toThrow('fileinto')
  expect(generateSieve([rule],['fileinto'])).toContain('"a\\"b\\\\c"')
  expect(()=>generateSieve([{...rule,destination:'INBOX\r\ndiscard;'}],['fileinto'])).toThrow('line breaks')
 })
 it('simulates supported header conditions only and respects stop',()=>{
  const first={...newSieveRule(),conditions:[{field:'from' as const,match:'is' as const,value:'a@example.test'}]}
  const second={...first,id:'second',action:'discard' as const}
  expect(simulateSieve([first,second],{from:['a@example.test'],to:[],subject:''})).toEqual([first])
  expect(simulateSieve([{...first,stop:false},second],{from:['a@example.test'],to:[],subject:''})).toHaveLength(2)
 })
})
