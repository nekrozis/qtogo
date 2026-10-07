/* glue.h -- qtogo's bridge to the vendored 7-Zip decoder.

   Part of qtogo, not of the LZMA SDK. It is the C boundary of this package, so it
   names no SDK type and no SDK header: a change inside the decoder cannot change
   this file.

   Types are chosen by what the value is, not by what the decoder happens to use:
   anything the 7z format fixes the width of is a fixed-width type (a size is 64
   bits, an item count, index and checksum are 32 bits, a UTF-16 unit is 16), a
   file descriptor is uintptr_t, and anything that measures memory this process can
   address stays size_t, because that is what the allocator takes. */

#ifndef QTOGO_LZMA_GLUE_H
#define QTOGO_LZMA_GLUE_H

#include <stddef.h>
#include <stdint.h>

/* The read buffer the decoder streams the archive through. It is charged to the
   caller's memory budget like any other allocation. */
#define QTGO_IN_BUF_SIZE ((size_t)1 << 18)

/* What qtogo_extract returns when it decoded the item. */
#define QTGO_OK 0

/* An open archive. What it holds is glue.c's business. */
typedef struct qtogo_archive qtogo_archive;

/* Prepares the decoder's tables; call once, before anything else. */
void qtogo_init(void);

/* Reads an archive from a descriptor the caller owns -- this code never closes
   it -- allowing the decoder to hold at most budget bytes. Returns NULL and sets
   *result on failure. */
qtogo_archive *qtogo_open(uintptr_t fd, size_t budget, int *result);

/* Releases everything and returns the bytes nobody freed: zero when the decoder
   kept nothing. */
size_t qtogo_close(qtogo_archive *a);

/* Bytes the decoder still holds. */
size_t qtogo_used(const qtogo_archive *a);

/* The largest decoded block the archive asks for, from its metadata. */
uint64_t qtogo_largest_block(const qtogo_archive *a);

uint32_t qtogo_num_items(const qtogo_archive *a);

/* Every call below takes an index below qtogo_num_items: the decoder indexes its
   own arrays with it and does not range-check, so the caller checks first. */
int qtogo_is_dir(const qtogo_archive *a, uint32_t i);
uint64_t qtogo_item_size(const qtogo_archive *a, uint32_t i);
int qtogo_item_has_crc(const qtogo_archive *a, uint32_t i);
uint32_t qtogo_item_crc(const qtogo_archive *a, uint32_t i);

/* The length of the item's name in UTF-16 units, terminator included. */
size_t qtogo_item_name_len(const qtogo_archive *a, uint32_t i);
void qtogo_item_name_copy(const qtogo_archive *a, uint32_t i, uint16_t *dest);

/* Points *data at the item's bytes inside the block buffer, which is kept between
   calls: they stay valid until the next call or until the archive is closed.
   Returns a result code. */
int qtogo_extract(qtogo_archive *a, uint32_t i, const uint8_t **data, size_t *size);

#endif
