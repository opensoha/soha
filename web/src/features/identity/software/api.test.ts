import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  deleteSoftwarePackage,
  getSoftwarePackageDownloadRecords,
  getSoftwareStorage,
  importSoftwarePackage,
  listSoftwarePackages,
  uploadSoftwarePackage,
} from './api'
import type {
  SoftwarePackage,
  SoftwarePackageUploadInput,
  SoftwarePackageURLImportInput,
} from './types'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  getEnvelope: vi.fn(),
  post: vi.fn(),
  upload: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const softwarePackage = {
  id: 'pkg/1',
  softwareId: 'soha',
  name: 'Soha Desktop',
  publisher: 'OpenSoha',
  version: '1.0.0',
  platform: 'darwin',
  arch: 'arm64',
  fileName: 'soha.pkg',
  sizeBytes: 7,
  sha256: 'a'.repeat(64),
  downloadPath: '/api/v1/software/packages/pkg%2F1/download',
  downloadCount: 0,
  createdAt: '2026-08-08T00:00:00Z',
  updatedAt: '2026-08-08T00:00:00Z',
} satisfies SoftwarePackage

describe('software package api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('keeps the paginated list envelope and normalizes filters', async () => {
    apiMocks.getEnvelope.mockResolvedValueOnce({ items: [softwarePackage], nextCursor: 'next' })

    await expect(
      listSoftwarePackages({
        platform: ' darwin ',
        arch: ' arm64 ',
        storageIntegrationId: ' storage-1 ',
      }),
    ).resolves.toEqual({
      items: [softwarePackage],
      nextCursor: 'next',
    })
    expect(apiMocks.getEnvelope).toHaveBeenCalledWith(
      '/software/packages?platform=darwin&arch=arm64&storageIntegrationId=storage-1&limit=200',
    )
  })

  it('uploads multipart fields and encodes delete ids', async () => {
    apiMocks.upload.mockResolvedValueOnce({ data: softwarePackage })
    const input: SoftwarePackageUploadInput = {
      storageIntegrationId: 'storage-1',
      softwareId: 'soha',
      name: 'Soha Desktop',
      publisher: 'OpenSoha',
      version: '1.0.0',
      platform: 'darwin',
      arch: 'arm64',
      file: new File(['payload'], 'soha.pkg'),
    }

    await expect(uploadSoftwarePackage(input)).resolves.toEqual(softwarePackage)
    const formData = apiMocks.upload.mock.calls[0]?.[1] as FormData
    expect(apiMocks.upload.mock.calls[0]?.[0]).toBe('/software/packages')
    expect(formData.get('softwareId')).toBe('soha')
    expect(formData.get('storageIntegrationId')).toBe('storage-1')
    expect(formData.get('file')).toBe(input.file)

    await deleteSoftwarePackage(softwarePackage.id)
    expect(apiMocks.delete).toHaveBeenCalledWith('/software/packages/pkg%2F1')
  })

  it('imports a remote package through the server', async () => {
    apiMocks.post.mockResolvedValueOnce({ data: softwarePackage })
    const input: SoftwarePackageURLImportInput = {
      softwareId: 'soha',
      name: 'Soha Desktop',
      publisher: 'OpenSoha',
      version: '1.0.0',
      platform: 'darwin',
      arch: 'arm64',
      url: 'https://downloads.example.com/soha.pkg',
      fileName: 'soha.pkg',
    }

    await expect(importSoftwarePackage(input)).resolves.toEqual(softwarePackage)
    expect(apiMocks.post).toHaveBeenCalledWith('/software/packages/import', input)
  })

  it('loads the managed storage summary', async () => {
    const storage = {
      backend: 'filesystem',
      objectCount: 1,
      totalBytes: 7,
      items: [softwarePackage],
    }
    apiMocks.getEnvelope.mockResolvedValueOnce({ data: storage })

    await expect(getSoftwareStorage(' storage-1 ')).resolves.toEqual(storage)
    expect(apiMocks.getEnvelope).toHaveBeenCalledWith(
      '/software/storage?limit=200&storageIntegrationId=storage-1',
    )
  })

  it('loads encoded package download records', async () => {
    apiMocks.getEnvelope.mockResolvedValueOnce({ items: [] })
    await expect(getSoftwarePackageDownloadRecords('pkg/1')).resolves.toEqual({ items: [] })
    expect(apiMocks.getEnvelope).toHaveBeenCalledWith(
      '/software/packages/pkg%2F1/download-records?limit=50',
    )
  })
})
