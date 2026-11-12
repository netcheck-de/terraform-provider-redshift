"""Reproduce mapping.parquet.base64 with Python 3.13 and pyarrow==23.0.1.

Terraform uploads the checked-in bytes; Python/Arrow is not required to deploy.
Run this script and redirect stdout to mapping.parquet.base64 to regenerate it.
"""

import base64

import pyarrow as pa
import pyarrow.parquet as pq

table = pa.table({
    "first": pa.array([11], type=pa.int32()),
    "second": pa.array([22], type=pa.int32()),
    "nullable": pa.array([None], type=pa.int32()),
})
sink = pa.BufferOutputStream()
pq.write_table(table, sink, version="1.0", compression="NONE", use_dictionary=False)
data = sink.getvalue()
assert pq.read_table(pa.BufferReader(data)).equals(table)
print(base64.b64encode(data).decode("ascii"))
