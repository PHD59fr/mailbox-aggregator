import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SMTPModal from './SMTPModal.vue'
import { createSMTPConfig, updateSMTPConfig } from '../api/accounts'

vi.mock('../api/accounts', () => ({ createSMTPConfig: vi.fn(), updateSMTPConfig: vi.fn() }))
beforeEach(() => { vi.resetAllMocks(); setActivePinia(createPinia()) })

describe('sending identity configuration', () => {
  const config = { id: 'existing', name: 'Work', sender_name: 'Support', sender_email: 'support@example.test', host: 'smtp.example.test', port: 2525, security: 'starttls' as const, username: 'smtp-user', password: 'stored-secret', is_default: true }

  it('prefills SMTP editing without exposing the stored password or resetting a custom port', async () => {
    vi.mocked(updateSMTPConfig).mockResolvedValue(undefined)
    const wrapper = mount(SMTPModal, { props: { modelValue: true, config } })
    expect(wrapper.find('h2').text()).toBe('Edit sender address')
    expect(wrapper.find<HTMLInputElement>('input[type="number"]').element.value).toBe('2525')
    const password = wrapper.find<HTMLInputElement>('input[type="password"]')
    expect(password.element.value).toBe('')
    expect(password.attributes('required')).toBeUndefined()
    expect(password.attributes('placeholder')).toContain('keep current password')
    expect(wrapper.html()).not.toContain(config.password)
    await wrapper.find('input').setValue('Updated work')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(updateSMTPConfig).toHaveBeenCalledExactlyOnceWith('existing', { name: 'Updated work', sender_name: config.sender_name, sender_email: config.sender_email, host: config.host, port: 2525, security: 'starttls', username: config.username, password: '' })
    expect(createSMTPConfig).not.toHaveBeenCalled()
    expect(config.name).toBe('Work')
  })

  it('accepts a replacement password and clears edited credentials on close', async () => {
    vi.mocked(updateSMTPConfig).mockResolvedValue(undefined)
    const wrapper = mount(SMTPModal, { props: { modelValue: true, config } })
    await wrapper.find('input[type="password"]').setValue(' new secret ')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(updateSMTPConfig).toHaveBeenCalledWith(config.id, expect.objectContaining({ password: ' new secret ' }))
    expect(wrapper.find<HTMLInputElement>('input[type="password"]').element.value).toBe('')
    await wrapper.setProps({ modelValue: false, config: null })
    await wrapper.setProps({ modelValue: true })
    expect(wrapper.find('h2').text()).toBe('Add sender address')
    expect(wrapper.find('input').element.value).toBe('')
    expect(wrapper.find('input[type="password"]').attributes()).toHaveProperty('required')
    expect(wrapper.find<HTMLInputElement>('input[type="number"]').element.value).toBe('465')
  })

  it('does not save on cancel and retains edited values after failure', async () => {
    vi.mocked(updateSMTPConfig).mockRejectedValue(new Error('Concurrent edit.'))
    const wrapper = mount(SMTPModal, { props: { modelValue: true, config } })
    await wrapper.find('input').setValue('Unsaved')
    await wrapper.find('.smtp-actions .secondary').trigger('click')
    expect(updateSMTPConfig).not.toHaveBeenCalled()
    expect(config.name).toBe('Work')
    await wrapper.setProps({ modelValue: false })
    await wrapper.setProps({ modelValue: true })
    expect(wrapper.find<HTMLInputElement>('input[type="number"]').element.value).toBe('2525')
    await wrapper.find('input').setValue('Updated')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toContain('Concurrent edit')
    expect(wrapper.find('input').element.value).toBe('Updated')
    expect(wrapper.emitted('success')).toBeUndefined()
  })

  it('creates a sending identity with SMTP credentials without requesting IMAP', async () => {
    const wrapper = mount(SMTPModal, { props: { modelValue: true } })
    const inputs = wrapper.findAll('input')
    for (const [index, value] of ['Support', 'Support team', 'support@example.test', 'smtp.example.test', 465, 'smtp-user', 'smtp-secret'].entries()) await inputs[index]!.setValue(value)
    expect(wrapper.findAll('label').every(label => !label.text().includes('IMAP'))).toBe(true)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(createSMTPConfig).toHaveBeenCalledWith({ name: 'Support', sender_name: 'Support team', sender_email: 'support@example.test', host: 'smtp.example.test', port: 465, security: 'tls', username: 'smtp-user', password: 'smtp-secret' })
    expect(wrapper.emitted('success')).toHaveLength(1)
    expect(wrapper.emitted('update:modelValue')).toEqual([[false]])
    expect((wrapper.find('input[type="password"]').element as HTMLInputElement).value).toBe('')
  })

  it('uses port 587 for STARTTLS and shows failed saves', async () => {
    vi.mocked(createSMTPConfig).mockRejectedValue(new Error('The file changed since it was read.'))
    const wrapper = mount(SMTPModal, { props: { modelValue: true } })
    await wrapper.find('select').setValue('starttls')
    expect((wrapper.find('input[type="number"]').element as HTMLInputElement).value).toBe('587')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toContain('file changed')
    expect(wrapper.emitted('success')).toBeUndefined()
  })
})
