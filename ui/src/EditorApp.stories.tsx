import type { Meta, StoryObj } from '@storybook/react-vite'
import { expect, fn, waitFor } from 'storybook/test'
import { parse } from 'yaml'
import EditorApp from './EditorApp'
import { type FilePickerWindow } from './editorDocument'
import { editorYamlFixture } from './test/editorYamlFixture'

const sample = {
  version: '1',
  name: 'Product & engineering',
  entries: [
    {
      id: 'api',
      term: 'API',
      aliases: [{ text: 'Application programming interface' }],
      senses: [
        {
          id: 'software',
          definition:
            'A defined way for software systems to **communicate** and exchange data.\n\nAn API describes what you can request, how to request it, and what to expect in return.',
          context: 'Software development',
          partOfSpeech: 'noun',
          examples: ['The mobile app uses our API to retrieve order details.'],
          tags: ['engineering', 'public'],
        },
      ],
    },
    {
      id: 'deployment',
      term: 'Deployment',
      senses: [
        { id: 'release', definition: 'The process of making a version of a service available in an environment.' },
      ],
    },
    {
      id: 'design-system',
      term: 'Design system',
      senses: [
        {
          id: 'product',
          definition: 'Shared components, patterns, and principles for building a consistent product experience.',
        },
      ],
    },
    {
      id: 'feature-flag',
      term: 'Feature flag',
      senses: [
        {
          id: 'release',
          definition: 'A switch that controls whether a feature is available without a new deployment.',
        },
      ],
    },
    {
      id: 'latency',
      term: 'Latency',
      senses: [{ id: 'network', definition: 'The time between a request and its response.' }],
    },
    {
      id: 'onboarding',
      term: 'Onboarding',
      senses: [{ id: 'product', definition: 'The experience that helps someone get started with a product.' }],
    },
    {
      id: 'release',
      term: 'Release',
      senses: [{ id: 'software', definition: 'A version of a product made available to its users.' }],
    },
    {
      id: 'service-level-objective',
      term: 'Service level objective',
      senses: [{ id: 'reliability', definition: 'A measurable target for the reliability of a service.' }],
    },
  ],
}
const writeFile = fn<(value: string) => Promise<void>>()
const closeFile = fn<() => Promise<void>>()

function mockLocalFile(name: string, initial: string) {
  writeFile.mockReset()
  closeFile.mockReset()
  const previous = (window as FilePickerWindow).showOpenFilePicker
  let text = initial
  ;(window as FilePickerWindow).showOpenFilePicker = async () => [
    {
      name,
      getFile: async () => new File([text], name),
      createWritable: async () => ({
        write: writeFile.mockImplementation(async (value) => {
          text = value
        }),
        close: closeFile,
        abort: async () => {},
      }),
    },
  ]
  return () => {
    ;(window as FilePickerWindow).showOpenFilePicker = previous
  }
}

const meta = {
  component: EditorApp,
  parameters: { layout: 'fullscreen' },
  tags: ['ai-generated'],
  args: { siteName: 'Jargonary' },
  beforeEach: () => mockLocalFile('product-engineering.json', JSON.stringify(sample, null, 2)),
} satisfies Meta<typeof EditorApp>
export default meta
type Story = StoryObj<typeof meta>
export const Start: Story = {}
export const Editing: Story = {
  play: async ({ canvas, userEvent }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Open glossary' }))
    await expect(await canvas.findByLabelText('Term', { exact: true })).toHaveValue('API')
  },
}

export const EditingWorkflow: Story = {
  play: async ({ canvas, userEvent }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Open glossary' }))
    const term = await canvas.findByLabelText('Term', { exact: true })
    await userEvent.clear(term)
    await userEvent.type(term, 'Application programming interface')
    await expect(term).toHaveFocus()
    const definition = canvas.getByRole('textbox', { name: 'Definition for meaning 1' })
    await userEvent.click(definition)
    await userEvent.type(definition, ' Additional context.', { skipClick: true })
    await expect(definition).toHaveTextContent('Additional context.')
    await userEvent.click(canvas.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(closeFile).toHaveBeenCalledOnce())
    const saved = JSON.parse(writeFile.mock.lastCall![0])
    await expect(saved.entries[0].term).toBe('Application programming interface')
    await expect(saved.entries[0].senses[0].definition).toContain('Additional context.')
    await expect(canvas.getByRole('button', { name: 'Save' })).toBeDisabled()
    await userEvent.type(canvas.getByLabelText('Find an entry'), 'Latency')
    await userEvent.click(canvas.getByRole('button', { name: 'Latency, 1 meaning' }))
    await expect(canvas.getByLabelText('Term', { exact: true })).toHaveValue('Latency')
    await userEvent.click(canvas.getByRole('switch', { name: 'Raw JSON' }))
    await expect(canvas.getByRole('region', { name: 'Raw JSON editor' })).toBeVisible()
    await userEvent.click(canvas.getByRole('button', { name: 'Form' }))
    await expect(canvas.getByLabelText('Term', { exact: true })).toHaveValue('Latency')
  },
}

export const YamlEditing: Story = {
  beforeEach: () => mockLocalFile('platform.yaml', editorYamlFixture),
  play: async ({ canvas, userEvent }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Open glossary' }))
    const term = await canvas.findByLabelText('Term', { exact: true })
    await expect(term).toHaveValue('API')
    await userEvent.click(canvas.getByRole('switch', { name: 'Raw YAML' }))
    await expect(canvas.getByRole('textbox', { name: 'Glossary YAML' })).toHaveTextContent(
      '# Owned by the platform team',
    )
    await userEvent.click(canvas.getByRole('button', { name: 'Form' }))
    await userEvent.clear(canvas.getByLabelText('Term', { exact: true }))
    await userEvent.type(canvas.getByLabelText('Term', { exact: true }), 'YAML API')
    await userEvent.click(canvas.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(closeFile).toHaveBeenCalledOnce())
    const saved = writeFile.mock.lastCall![0]
    await expect(parse(saved).entries[0].term).toBe('YAML API')
    await expect(saved).toContain("term: 'YAML API' # Preferred name")
    await expect(saved).toContain('# Owned by the platform team')
    await expect(canvas.getByRole('button', { name: 'Save' })).toBeDisabled()
    await userEvent.click(canvas.getByRole('switch', { name: 'Raw YAML' }))
    await expect(canvas.getByRole('textbox', { name: 'Glossary YAML' })).toBeVisible()
  },
}

export const NewGlossary: Story = {
  beforeEach: () => {
    writeFile.mockReset()
    closeFile.mockReset()
    const previous = (window as FilePickerWindow).showSaveFilePicker
    let text = ''
    ;(window as FilePickerWindow).showSaveFilePicker = async () => ({
      name: 'new-glossary.yaml',
      getFile: async () => new File([text], 'new-glossary.yaml'),
      createWritable: async () => ({
        write: writeFile.mockImplementation(async (value) => {
          text = value
        }),
        close: closeFile,
        abort: async () => {},
      }),
    })
    return () => {
      ;(window as FilePickerWindow).showSaveFilePicker = previous
    }
  },
  play: async ({ canvas, userEvent }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'New glossary' }))
    const name = canvas.getByLabelText('Glossary name')
    await expect(name).toHaveFocus()
    await userEvent.clear(name)
    await userEvent.type(name, 'A new team glossary')
    await userEvent.selectOptions(canvas.getByLabelText('New glossary format'), 'yaml')
    await expect(writeFile).not.toHaveBeenCalled()
    await expect(canvas.getByRole('button', { name: 'Save' })).toBeEnabled()
    await userEvent.click(canvas.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(closeFile).toHaveBeenCalledOnce())
    await expect(parse(writeFile.mock.lastCall![0])).toEqual({
      version: '1',
      name: 'A new team glossary',
      entries: [],
    })
    await expect(canvas.getByRole('button', { name: 'Save' })).toBeDisabled()
    await userEvent.click(canvas.getByRole('switch', { name: 'Raw YAML' }))
    await expect(canvas.getByRole('textbox', { name: 'Glossary YAML' })).toBeVisible()
  },
}
