import {defineStore} from 'pinia'
export const useSieveSeed=defineStore('sieve-seed',{state:()=>({seed:null as null|{accountID:string;from:string[];to:string[];subject:string}}),actions:{clear(){this.seed=null}}})
