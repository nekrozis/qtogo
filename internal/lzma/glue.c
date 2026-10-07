/* glue.c -- qtogo's bridge to the vendored 7-Zip decoder.

   Part of qtogo, not of the LZMA SDK. This is where the SDK's own types live: the
   header names none of them, so an upgrade changes this file and nothing else. */

#include <stdlib.h>
#include <string.h>

#include "7z.h"
#include "7zAlloc.h"
#include "7zCrc.h"
#include "7zFile.h"
#include "7zTypes.h"

#include "glue.h"

/* The decoder allocates through a callback, so a budget applies where the memory
   is taken: the archive header, the name table and the solid block buffer all pass
   through here. Each block carries its size in a header, so the live total is
   exact and "everything was released" is checkable. */
typedef struct
{
  ISzAlloc vt;
  size_t budget;
  size_t used;
} qtogo_alloc;

struct qtogo_archive
{
  CSzArEx db;
  CFileInStream file;
  CLookToRead2 look;
  qtogo_alloc alloc;
  Byte *inBuf;
  UInt32 blockIndex;
  Byte *outBuffer;
  size_t outBufferSize;
};

/* Each block carries its size in a header, padded to the platform's widest
   alignment: the decoder stores UInt64 arrays through this callback, and a bare
   size_t header would leave them under-aligned on an ABI where uint64_t is more
   strictly aligned than size_t. */
typedef union
{
  size_t size;
  max_align_t align;
} qtogo_header;

static void *qtogo_alloc_do(ISzAllocPtr p, size_t size)
{
  qtogo_alloc *a = (qtogo_alloc *)p;
  qtogo_header *raw;

  /* Two checks rather than one subtraction: when the accounting ever went wrong,
     `budget - used` would underflow and read as "there is room". */
  if (a->used > a->budget)
    return NULL;
  if (size > a->budget - a->used)
    return NULL;

  /* The size header is added after the budget check, so the addition needs its
     own guard against wrapping. */
  if (size > SIZE_MAX - sizeof(qtogo_header))
    return NULL;

  raw = (qtogo_header *)malloc(sizeof(qtogo_header) + size);
  if (!raw)
    return NULL;
  raw->size = size;
  a->used += size;
  return (void *)((char *)raw + sizeof(qtogo_header));
}

static void qtogo_alloc_undo(ISzAllocPtr p, void *addr)
{
  qtogo_alloc *a = (qtogo_alloc *)p;
  qtogo_header *raw;

  if (!addr)
    return;
  raw = (qtogo_header *)((char *)addr - sizeof(qtogo_header));
  a->used -= raw->size;
  free(raw);
}

void qtogo_init(void)
{
  CrcGenerateTable();
}

qtogo_archive *qtogo_open(uintptr_t fd, size_t budget, int *result)
{
  qtogo_archive *a = (qtogo_archive *)calloc(1, sizeof(qtogo_archive));
  if (!a)
  {
    *result = SZ_ERROR_MEM;
    return NULL;
  }

  a->alloc.vt.Alloc = qtogo_alloc_do;
  a->alloc.vt.Free = qtogo_alloc_undo;
  a->alloc.budget = budget;
  a->alloc.used = 0;

  /* The descriptor belongs to the caller. */
  #ifdef _WIN32
    a->file.file.handle = (HANDLE)fd;
  #else
    a->file.file.fd = (int)fd;
  #endif
  FileInStream_CreateVTable(&a->file);
  a->file.wres = 0;

  LookToRead2_CreateVTable(&a->look, 0);
  a->inBuf = (Byte *)qtogo_alloc_do(&a->alloc.vt, QTGO_IN_BUF_SIZE);
  if (!a->inBuf)
  {
    free(a);
    *result = SZ_ERROR_MEM;
    return NULL;
  }
  a->look.buf = a->inBuf;
  a->look.bufSize = QTGO_IN_BUF_SIZE;
  a->look.realStream = &a->file.vt;
  LookToRead2_INIT(&a->look)

  SzArEx_Init(&a->db);
  *result = (int)SzArEx_Open(&a->db, &a->look.vt, &a->alloc.vt, &a->alloc.vt);
  if (*result != SZ_OK)
  {
    SzArEx_Free(&a->db, &a->alloc.vt);
    qtogo_alloc_undo(&a->alloc.vt, a->inBuf);
    free(a);
    return NULL;
  }
  return a;
}

size_t qtogo_close(qtogo_archive *a)
{
  size_t leaked;

  if (!a)
    return 0;
  SzArEx_Free(&a->db, &a->alloc.vt);
  if (a->outBuffer)
    qtogo_alloc_undo(&a->alloc.vt, a->outBuffer);
  qtogo_alloc_undo(&a->alloc.vt, a->inBuf);
  leaked = a->alloc.used;
  free(a);
  return leaked;
}

size_t qtogo_used(const qtogo_archive *a)
{
  return a->alloc.used;
}

uint64_t qtogo_largest_block(const qtogo_archive *a)
{
  UInt64 largest = 0;
  UInt32 i;

  for (i = 0; i < a->db.db.NumFolders; i++)
  {
    UInt64 size = SzAr_GetFolderUnpackSize(&a->db.db, i);
    if (size > largest)
      largest = size;
  }
  return (uint64_t)largest;
}

uint32_t qtogo_num_items(const qtogo_archive *a)
{
  return (uint32_t)a->db.NumFiles;
}

/* The SDK's accessors are macros, so they need a function to be callable. */
int qtogo_is_dir(const qtogo_archive *a, uint32_t i)
{
  return SzArEx_IsDir(&a->db, i) ? 1 : 0;
}

uint64_t qtogo_item_size(const qtogo_archive *a, uint32_t i)
{
  return (uint64_t)SzArEx_GetFileSize(&a->db, i);
}

int qtogo_item_has_crc(const qtogo_archive *a, uint32_t i)
{
  return SzBitWithVals_Check(&a->db.CRCs, i) ? 1 : 0;
}

uint32_t qtogo_item_crc(const qtogo_archive *a, uint32_t i)
{
  return (uint32_t)a->db.CRCs.Vals[i];
}

size_t qtogo_item_name_len(const qtogo_archive *a, uint32_t i)
{
  return SzArEx_GetFileNameUtf16(&a->db, i, NULL);
}

void qtogo_item_name_copy(const qtogo_archive *a, uint32_t i, uint16_t *dest)
{
  SzArEx_GetFileNameUtf16(&a->db, i, (UInt16 *)dest);
}

int qtogo_extract(qtogo_archive *a, uint32_t i, const uint8_t **data, size_t *size)
{
  size_t offset = 0;
  SRes res = SzArEx_Extract(&a->db, &a->look.vt, i,
      &a->blockIndex, &a->outBuffer, &a->outBufferSize,
      &offset, size, &a->alloc.vt, &a->alloc.vt);

  if (res != SZ_OK)
    return (int)res;
  *data = (const uint8_t *)(a->outBuffer + offset);
  return SZ_OK;
}
