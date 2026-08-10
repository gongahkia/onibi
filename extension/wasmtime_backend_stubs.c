#include <caml/alloc.h>
#include <caml/custom.h>
#include <caml/fail.h>
#include <caml/memory.h>
#include <caml/mlvalues.h>
#include <caml/signals.h>
#include <caml/threads.h>

#include <errno.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include <wasmtime.h>

#define ZENBU_MAX_COMPONENT_LIST_ITEMS 4096
#define ZENBU_MAX_COMPONENT_RECORD_FIELDS 64
#define ZENBU_MAX_COMPONENT_STRING_BYTES 1048576
#define ZENBU_MAX_COMPONENT_VALUE_DEPTH 64

/*
 * This file is the complete native boundary for M9.  Do not expose any of the
 * types below from zenbu.extension: the OCaml side only exchanges immutable
 * Extension_value data and opaque generation handles.
 */

typedef struct {
  wasm_engine_t *engine;
  wasmtime_store_t *store;
  wasmtime_component_linker_t *linker;
  wasmtime_component_t *component;
  wasmtime_component_func_t register_func;
  wasmtime_component_func_t invoke_func;
  int has_register;
  int has_invoke;
  int disposed;
  uint64_t fuel;
  uint64_t compile_microseconds;
  uint64_t instantiate_microseconds;
  uint64_t last_call_microseconds;
  uint64_t last_fuel_consumed;
} zenbu_wasmtime_runtime;

static uint64_t elapsed_microseconds(clock_t started) {
  clock_t finished = clock();
  if (finished <= started) return 0;
  return ((uint64_t)(finished - started) * UINT64_C(1000000)) /
      (uint64_t)CLOCKS_PER_SEC;
}

static void record_call_metrics(zenbu_wasmtime_runtime *runtime,
                                clock_t started) {
  runtime->last_call_microseconds = elapsed_microseconds(started);
  uint64_t remaining = 0;
  wasmtime_error_t *error = wasmtime_context_get_fuel(
      wasmtime_store_context(runtime->store), &remaining);
  if (error != NULL) {
    wasmtime_error_delete(error);
    runtime->last_fuel_consumed = 0;
  } else {
    runtime->last_fuel_consumed =
        remaining <= runtime->fuel ? runtime->fuel - remaining : 0;
  }
}

static void runtime_dispose(zenbu_wasmtime_runtime *runtime) {
  if (runtime == NULL || runtime->disposed) return;
  runtime->disposed = 1;
  if (runtime->linker != NULL) wasmtime_component_linker_delete(runtime->linker);
  if (runtime->store != NULL) wasmtime_store_delete(runtime->store);
  if (runtime->component != NULL) wasmtime_component_delete(runtime->component);
  if (runtime->engine != NULL) wasm_engine_delete(runtime->engine);
  runtime->linker = NULL;
  runtime->store = NULL;
  runtime->component = NULL;
  runtime->engine = NULL;
}

static void runtime_finalize(value handle) {
  zenbu_wasmtime_runtime *runtime =
      *((zenbu_wasmtime_runtime **)Data_custom_val(handle));
  runtime_dispose(runtime);
  free(runtime);
  *((zenbu_wasmtime_runtime **)Data_custom_val(handle)) = NULL;
}

static struct custom_operations runtime_operations = {
    "zenbu.wasmtime.component-runtime",
    runtime_finalize,
    custom_compare_default,
    custom_hash_default,
    custom_serialize_default,
    custom_deserialize_default,
    custom_compare_ext_default,
    custom_fixed_length_default,
};

static zenbu_wasmtime_runtime *runtime_of(value handle) {
  return *((zenbu_wasmtime_runtime **)Data_custom_val(handle));
}

static char *copy_text(const char *data, size_t len) {
  char *text = malloc(len + 1);
  if (text == NULL) return NULL;
  memcpy(text, data, len);
  text[len] = 0;
  return text;
}

static char *wasmtime_message(wasmtime_error_t *error) {
  wasm_name_t message;
  if (error == NULL) return copy_text("unknown Wasmtime error", 22);
  wasmtime_error_message(error, &message);
  char *result = copy_text(message.data, message.size);
  wasm_byte_vec_delete(&message);
  wasmtime_error_delete(error);
  return result == NULL ? copy_text("out of memory", 13) : result;
}

static char *errno_message(const char *prefix, const char *path) {
  const char *detail = strerror(errno);
  size_t len = strlen(prefix) + strlen(path) + strlen(detail) + 4;
  char *message = malloc(len);
  if (message == NULL) return copy_text("out of memory", 13);
  snprintf(message, len, "%s %s: %s", prefix, path, detail);
  return message;
}

static char *read_file(const char *path, uint8_t **bytes, size_t *length) {
  FILE *file = fopen(path, "rb");
  if (file == NULL) return errno_message("cannot read", path);
  if (fseek(file, 0, SEEK_END) != 0) {
    char *error = errno_message("cannot seek", path);
    fclose(file);
    return error;
  }
  long size = ftell(file);
  if (size < 0) {
    char *error = errno_message("cannot size", path);
    fclose(file);
    return error;
  }
  if (fseek(file, 0, SEEK_SET) != 0) {
    char *error = errno_message("cannot seek", path);
    fclose(file);
    return error;
  }
  uint8_t *buffer = malloc((size_t)size == 0 ? 1 : (size_t)size);
  if (buffer == NULL) {
    fclose(file);
    return copy_text("out of memory", 13);
  }
  size_t read = fread(buffer, 1, (size_t)size, file);
  if (read != (size_t)size || ferror(file)) {
    char *error = errno_message("cannot read", path);
    free(buffer);
    fclose(file);
    return error;
  }
  fclose(file);
  *bytes = buffer;
  *length = (size_t)size;
  return NULL;
}

static value string_value(const char *data, size_t len) {
  value result = caml_alloc_string(len);
  memcpy(Bytes_val(result), data, len);
  return result;
}

static value result_ok(value payload) {
  value result = caml_alloc(1, 0);
  Store_field(result, 0, payload);
  return result;
}

static value result_error_text(const char *message) {
  CAMLparam0();
  CAMLlocal2(text, result);
  text = caml_copy_string(message == NULL ? "unknown Wasmtime error" : message);
  result = caml_alloc(1, 1);
  Store_field(result, 0, text);
  CAMLreturn(result);
}

static int extension_tag(value source) {
  if (Is_long(source)) return 0;
  return Tag_val(source) + 1;
}

static int component_from_extension(value source, wasmtime_component_val_t *out,
                                    char **error);

static int component_list_from_extension(value values,
                                         wasmtime_component_val_t *out,
                                         char **error) {
  size_t count = 0;
  value cursor = values;
  while (cursor != Val_int(0)) {
    count++;
    cursor = Field(cursor, 1);
  }
  wasmtime_component_val_t *items =
      calloc(count == 0 ? 1 : count, sizeof(wasmtime_component_val_t));
  if (items == NULL) {
    *error = copy_text("out of memory", 13);
    return 0;
  }
  cursor = values;
  for (size_t i = 0; i < count; i++) {
    if (!component_from_extension(Field(cursor, 0), &items[i], error)) {
      for (size_t j = 0; j < i; j++) wasmtime_component_val_delete(&items[j]);
      free(items);
      return 0;
    }
    cursor = Field(cursor, 1);
  }
  out->kind = WASMTIME_COMPONENT_LIST;
  wasmtime_component_vallist_new(&out->of.list, count, items);
  /* The v47 C API vector constructor moves, rather than clones, Component
     values from [items]. Only free the outer C array after that transfer. */
  free(items);
  return 1;
}

static int component_record_from_extension(value fields,
                                           wasmtime_component_val_t *out,
                                           char **error) {
  size_t count = 0;
  value cursor = fields;
  while (cursor != Val_int(0)) {
    count++;
    cursor = Field(cursor, 1);
  }
  wasmtime_component_valrecord_entry_t *entries =
      calloc(count == 0 ? 1 : count, sizeof(wasmtime_component_valrecord_entry_t));
  if (entries == NULL) {
    *error = copy_text("out of memory", 13);
    return 0;
  }
  cursor = fields;
  for (size_t i = 0; i < count; i++) {
    value pair = Field(cursor, 0);
    value key = Field(pair, 0);
    entries[i].name.size = caml_string_length(key);
    entries[i].name.data = malloc(entries[i].name.size == 0 ? 1 : entries[i].name.size);
    if (entries[i].name.data == NULL) {
      *error = copy_text("out of memory", 13);
      for (size_t j = 0; j < i; j++) {
        wasm_byte_vec_delete(&entries[j].name);
        wasmtime_component_val_delete(&entries[j].val);
      }
      free(entries);
      return 0;
    }
    memcpy(entries[i].name.data, String_val(key), entries[i].name.size);
    value field = Field(pair, 1);
    if (strcmp(String_val(key), "kind") == 0 && extension_tag(field) == 4) {
      entries[i].val.kind = WASMTIME_COMPONENT_ENUM;
      wasm_byte_vec_new(&entries[i].val.of.enumeration,
          caml_string_length(Field(field, 0)), String_val(Field(field, 0)));
    } else if (strcmp(String_val(key), "index") == 0 &&
               extension_tag(field) == 2) {
      intnat index = Long_val(Field(field, 0));
      if (index < 0 || (uint64_t)index > UINT32_MAX) {
        *error = copy_text("WIT path index is outside u32 range", 35);
        wasm_byte_vec_delete(&entries[i].name);
        for (size_t j = 0; j < i; j++) {
          wasm_byte_vec_delete(&entries[j].name);
          wasmtime_component_val_delete(&entries[j].val);
        }
        free(entries);
        return 0;
      }
      entries[i].val.kind = WASMTIME_COMPONENT_U32;
      entries[i].val.of.u32 = (uint32_t)index;
    } else if (!component_from_extension(field, &entries[i].val, error)) {
      wasm_byte_vec_delete(&entries[i].name);
      for (size_t j = 0; j < i; j++) {
        wasm_byte_vec_delete(&entries[j].name);
        wasmtime_component_val_delete(&entries[j].val);
      }
      free(entries);
      return 0;
    }
    cursor = Field(cursor, 1);
  }
  out->kind = WASMTIME_COMPONENT_RECORD;
  wasmtime_component_valrecord_new(&out->of.record, count, entries);
  /* As above, ownership of every nested name/value moved to [out]. */
  free(entries);
  return 1;
}

static int component_from_extension(value source, wasmtime_component_val_t *out,
                                    char **error) {
  memset(out, 0, sizeof(*out));
  switch (extension_tag(source)) {
    case 0:
      *error = copy_text("raw component values cannot contain nil", 39);
      return 0;
    case 1:
      out->kind = WASMTIME_COMPONENT_BOOL;
      out->of.boolean = Bool_val(Field(source, 0));
      return 1;
    case 2:
      out->kind = WASMTIME_COMPONENT_S64;
      out->of.s64 = (int64_t)Long_val(Field(source, 0));
      return 1;
    case 3:
      out->kind = WASMTIME_COMPONENT_F64;
      out->of.f64 = Double_val(Field(source, 0));
      return 1;
    case 4:
      out->kind = WASMTIME_COMPONENT_STRING;
      wasm_byte_vec_new(&out->of.string, caml_string_length(Field(source, 0)),
          String_val(Field(source, 0)));
      return 1;
    case 5:
      return component_list_from_extension(Field(source, 0), out, error);
    case 6:
      return component_record_from_extension(Field(source, 0), out, error);
    default:
      *error = copy_text("unsupported extension value", 27);
      return 0;
  }
}

static value extension_from_raw_component(const wasmtime_component_val_t *source,
                                          unsigned depth, char **error);

static value extension_list_from_component(const wasmtime_component_vallist_t *list,
                                           unsigned depth, char **error) {
  CAMLparam0();
  CAMLlocal3(result, item, cell);
  if (depth > ZENBU_MAX_COMPONENT_VALUE_DEPTH) {
    *error = copy_text("component response exceeds Zenbu limit: value nesting is too deep",
                       strlen("component response exceeds Zenbu limit: value nesting is too deep"));
    CAMLreturn(Val_int(0));
  }
  if (list->size > ZENBU_MAX_COMPONENT_LIST_ITEMS) {
    *error = copy_text("component response exceeds Zenbu limit: list has too many items",
                       strlen("component response exceeds Zenbu limit: list has too many items"));
    CAMLreturn(Val_int(0));
  }
  result = Val_int(0);
  for (size_t i = list->size; i > 0; i--) {
    item = extension_from_raw_component(&list->data[i - 1], depth + 1, error);
    if (*error != NULL) CAMLreturn(Val_int(0));
    cell = caml_alloc(2, 0);
    Store_field(cell, 0, item);
    Store_field(cell, 1, result);
    result = cell;
  }
  CAMLreturn(result);
}

static value extension_record_from_component(
    const wasmtime_component_valrecord_t *record, unsigned depth, char **error) {
  CAMLparam0();
  CAMLlocal5(result, key, item, pair, cell);
  if (depth > ZENBU_MAX_COMPONENT_VALUE_DEPTH) {
    *error = copy_text("component response exceeds Zenbu limit: value nesting is too deep",
                       strlen("component response exceeds Zenbu limit: value nesting is too deep"));
    CAMLreturn(Val_int(0));
  }
  if (record->size > ZENBU_MAX_COMPONENT_RECORD_FIELDS) {
    *error = copy_text("component response exceeds Zenbu limit: record has too many fields",
                       strlen("component response exceeds Zenbu limit: record has too many fields"));
    CAMLreturn(Val_int(0));
  }
  result = Val_int(0);
  for (size_t i = record->size; i > 0; i--) {
    const wasmtime_component_valrecord_entry_t *entry = &record->data[i - 1];
    if (entry->name.size > ZENBU_MAX_COMPONENT_STRING_BYTES) {
      *error = copy_text("component response exceeds Zenbu limit: record field is too large",
                         strlen("component response exceeds Zenbu limit: record field is too large"));
      CAMLreturn(Val_int(0));
    }
    key = string_value(entry->name.data, entry->name.size);
    item = extension_from_raw_component(&entry->val, depth + 1, error);
    if (*error != NULL) CAMLreturn(Val_int(0));
    pair = caml_alloc(2, 0);
    Store_field(pair, 0, key);
    Store_field(pair, 1, item);
    cell = caml_alloc(2, 0);
    Store_field(cell, 0, pair);
    Store_field(cell, 1, result);
    result = cell;
  }
  value output = caml_alloc(1, 5);
  Store_field(output, 0, result);
  CAMLreturn(output);
}

static value extension_from_raw_component(const wasmtime_component_val_t *source,
                                          unsigned depth, char **error) {
  CAMLparam0();
  CAMLlocal2(output, payload);
  if (depth > ZENBU_MAX_COMPONENT_VALUE_DEPTH) {
    *error = copy_text("component response exceeds Zenbu limit: value nesting is too deep",
                       strlen("component response exceeds Zenbu limit: value nesting is too deep"));
    CAMLreturn(Val_int(0));
  }
  switch (source->kind) {
    case WASMTIME_COMPONENT_BOOL:
      output = caml_alloc(1, 0);
      Store_field(output, 0, Val_bool(source->of.boolean));
      CAMLreturn(output);
    case WASMTIME_COMPONENT_S64:
      if (source->of.s64 > Max_long || source->of.s64 < Min_long) {
        *error = copy_text("component integer is outside OCaml int range", 44);
        CAMLreturn(Val_int(0));
      }
      output = caml_alloc(1, 1);
      Store_field(output, 0, Val_long(source->of.s64));
      CAMLreturn(output);
    case WASMTIME_COMPONENT_U32:
      output = caml_alloc(1, 1);
      Store_field(output, 0, Val_long(source->of.u32));
      CAMLreturn(output);
    case WASMTIME_COMPONENT_F64:
      output = caml_alloc(1, 2);
      Store_field(output, 0, caml_copy_double(source->of.f64));
      CAMLreturn(output);
    case WASMTIME_COMPONENT_STRING:
      if (source->of.string.size > ZENBU_MAX_COMPONENT_STRING_BYTES) {
        *error = copy_text("component response exceeds Zenbu limit: string is too large",
                           strlen("component response exceeds Zenbu limit: string is too large"));
        CAMLreturn(Val_int(0));
      }
      output = caml_alloc(1, 3);
      Store_field(output, 0,
          string_value(source->of.string.data, source->of.string.size));
      CAMLreturn(output);
    case WASMTIME_COMPONENT_LIST:
      payload = extension_list_from_component(&source->of.list, depth + 1, error);
      if (*error != NULL) CAMLreturn(Val_int(0));
      output = caml_alloc(1, 4);
      Store_field(output, 0, payload);
      CAMLreturn(output);
    case WASMTIME_COMPONENT_RECORD:
      CAMLreturn(extension_record_from_component(&source->of.record, depth + 1, error));
    case WASMTIME_COMPONENT_ENUM:
      if (source->of.enumeration.size > ZENBU_MAX_COMPONENT_STRING_BYTES) {
        *error = copy_text("component response exceeds Zenbu limit: enum is too large",
                           strlen("component response exceeds Zenbu limit: enum is too large"));
        CAMLreturn(Val_int(0));
      }
      output = caml_alloc(1, 3);
      Store_field(output, 0, string_value(source->of.enumeration.data,
          source->of.enumeration.size));
      CAMLreturn(output);
    default:
      *error = copy_text("unsupported Component value returned by plugin", 46);
      CAMLreturn(Val_int(0));
  }
}

static value extension_from_value_result(const wasmtime_component_val_t *source,
                                         char **error) {
  if (source->kind != WASMTIME_COMPONENT_RESULT) {
    *error = copy_text("plugin export must return result<value, string>", 47);
    return Val_int(0);
  }
  if (!source->of.result.is_ok) {
    const wasmtime_component_val_t *message = source->of.result.val;
    if (message != NULL && message->kind == WASMTIME_COMPONENT_STRING)
      *error = copy_text(message->of.string.data, message->of.string.size);
    else
      *error = copy_text("plugin returned an invalid runtime error", 40);
    return Val_int(0);
  }
  if (source->of.result.val == NULL) return Val_int(0);
  return extension_from_raw_component(source->of.result.val, 0, error);
}

static char *find_control_export(zenbu_wasmtime_runtime *runtime,
                                 const wasmtime_component_instance_t *instance,
                                 const char *name,
                                 wasmtime_component_func_t *func) {
  wasmtime_context_t *context = wasmtime_store_context(runtime->store);
  const char *interface = "zenbu:plugin/control@1.0.0";
  wasmtime_component_export_index_t *control =
      wasmtime_component_instance_get_export_index(instance, context, NULL,
          interface, strlen(interface));
  if (control == NULL)
    return copy_text("component does not export zenbu:plugin/control@1.0.0", 52);
  wasmtime_component_export_index_t *function =
      wasmtime_component_instance_get_export_index(instance, context, control,
          name, strlen(name));
  wasmtime_component_export_index_delete(control);
  if (function == NULL) {
    size_t len = strlen(name) + 47;
    char *message = malloc(len);
    if (message != NULL)
      snprintf(message, len, "component control export is missing %s", name);
    return message == NULL ? copy_text("out of memory", 13) : message;
  }
  int found = wasmtime_component_instance_get_func(instance, context, function,
      func);
  wasmtime_component_export_index_delete(function);
  if (!found) {
    size_t len = strlen(name) + 46;
    char *message = malloc(len);
    if (message != NULL)
      snprintf(message, len, "component control export %s is not a function",
          name);
    return message == NULL ? copy_text("out of memory", 13) : message;
  }
  return NULL;
}

CAMLprim value caml_zenbu_wasmtime_load(value entrypoint, value capabilities,
                                        value fuel, value memory_bytes) {
  CAMLparam4(entrypoint, capabilities, fuel, memory_bytes);
  CAMLlocal2(handle, result);
  zenbu_wasmtime_runtime *runtime = calloc(1, sizeof(*runtime));
  uint8_t *bytes = NULL;
  size_t length = 0;
  char *message = NULL;
  /* Extension_host projects capability-checked copies into each invocation
     request. This v1 Component world imports no host interfaces at all: an
     undeclared WASI or Zenbu host import is therefore a linker error rather
     than ambient authority. */
  (void)capabilities;
  if (runtime == NULL) CAMLreturn(result_error_text("out of memory"));
  message = read_file(String_val(entrypoint), &bytes, &length);
  if (message != NULL) goto error;
  wasm_config_t *config = wasm_config_new();
  if (config == NULL) {
    message = copy_text("cannot allocate Wasmtime configuration", 38);
    goto error;
  }
  wasmtime_config_wasm_component_model_set(config, true);
  wasmtime_config_consume_fuel_set(config, true);
  runtime->engine = wasm_engine_new_with_config(config);
  if (runtime->engine == NULL) {
    message = copy_text("cannot create Wasmtime engine", 29);
    goto error;
  }
  clock_t compile_started = clock();
  wasmtime_error_t *wasmtime_error = wasmtime_component_new(runtime->engine,
      bytes, length, &runtime->component);
  runtime->compile_microseconds = elapsed_microseconds(compile_started);
  free(bytes);
  bytes = NULL;
  if (wasmtime_error != NULL) {
    message = wasmtime_message(wasmtime_error);
    goto error;
  }
  runtime->store = wasmtime_store_new(runtime->engine, NULL, NULL);
  if (runtime->store == NULL) {
    message = copy_text("cannot create Wasmtime store", 28);
    goto error;
  }
  wasmtime_store_limiter(runtime->store, Long_val(memory_bytes), 10000, 16,
      64, 64);
  runtime->fuel = (uint64_t)Long_val(fuel);
  wasmtime_error = wasmtime_context_set_fuel(
      wasmtime_store_context(runtime->store), runtime->fuel);
  if (wasmtime_error != NULL) {
    message = wasmtime_message(wasmtime_error);
    goto error;
  }
  runtime->linker = wasmtime_component_linker_new(runtime->engine);
  if (runtime->linker == NULL) {
    message = copy_text("cannot create Wasmtime Component linker", 39);
    goto error;
  }
  wasmtime_component_instance_t instance;
  clock_t instantiate_started = clock();
  wasmtime_error = wasmtime_component_linker_instantiate(runtime->linker,
      wasmtime_store_context(runtime->store), runtime->component, &instance);
  runtime->instantiate_microseconds = elapsed_microseconds(instantiate_started);
  if (wasmtime_error != NULL) {
    message = wasmtime_message(wasmtime_error);
    goto error;
  }
  message = find_control_export(runtime, &instance, "register",
      &runtime->register_func);
  if (message != NULL) goto error;
  runtime->has_register = 1;
  message = find_control_export(runtime, &instance, "invoke",
      &runtime->invoke_func);
  if (message != NULL) goto error;
  runtime->has_invoke = 1;
  handle = caml_alloc_custom(&runtime_operations,
      sizeof(zenbu_wasmtime_runtime *), 0, 1);
  *((zenbu_wasmtime_runtime **)Data_custom_val(handle)) = runtime;
  result = result_ok(handle);
  CAMLreturn(result);

error:
  free(bytes);
  runtime_dispose(runtime);
  free(runtime);
  result = result_error_text(message);
  free(message);
  CAMLreturn(result);
}

static char *reset_fuel(zenbu_wasmtime_runtime *runtime) {
  wasmtime_error_t *error = wasmtime_context_set_fuel(
      wasmtime_store_context(runtime->store), runtime->fuel);
  return error == NULL ? NULL : wasmtime_message(error);
}

CAMLprim value caml_zenbu_wasmtime_register(value handle) {
  CAMLparam1(handle);
  CAMLlocal2(payload, result);
  zenbu_wasmtime_runtime *runtime = runtime_of(handle);
  if (runtime == NULL || runtime->disposed)
    CAMLreturn(result_error_text("Wasm plugin generation is disposed"));
  char *message = reset_fuel(runtime);
  if (message != NULL) {
    result = result_error_text(message);
    free(message);
    CAMLreturn(result);
  }
  /* Result slots are always initialised. See ADR 0026 for the v47 C API
     implementation/header mismatch that makes uninitialised storage unsafe. */
  wasmtime_component_val_t output = {
      .kind = WASMTIME_COMPONENT_BOOL,
      .of.boolean = false,
  };
  clock_t call_started = clock();
  wasmtime_error_t *error = wasmtime_component_func_call(&runtime->register_func,
      wasmtime_store_context(runtime->store), NULL, 0, &output, 1);
  record_call_metrics(runtime, call_started);
  if (error != NULL) {
    message = wasmtime_message(error);
    result = result_error_text(message);
    free(message);
    CAMLreturn(result);
  }
  if (output.kind != WASMTIME_COMPONENT_RESULT || !output.of.result.is_ok) {
    char detail[96];
    snprintf(detail, sizeof(detail),
        "component register must return an ok result (got Component kind %u)",
        output.kind);
    if (output.kind == WASMTIME_COMPONENT_RESULT && output.of.result.val != NULL &&
        output.of.result.val->kind == WASMTIME_COMPONENT_STRING)
      message = copy_text(output.of.result.val->of.string.data,
          output.of.result.val->of.string.size);
    else
      message = copy_text("component register returned an invalid result", 45);
    wasmtime_component_val_delete(&output);
    result = result_error_text(message == NULL ? detail : message);
    if (message == NULL) message = copy_text(detail, strlen(detail));
    free(message);
    CAMLreturn(result);
  }
  if (output.of.result.val == NULL ||
      output.of.result.val->kind != WASMTIME_COMPONENT_LIST) {
    char detail[128];
    snprintf(detail, sizeof(detail),
        "component register must return a list (got payload Component kind %u)",
        output.of.result.val == NULL ? UINT8_MAX : output.of.result.val->kind);
    wasmtime_component_val_delete(&output);
    CAMLreturn(result_error_text(detail));
  }
  char *conversion_error = NULL;
  payload = extension_list_from_component(&output.of.result.val->of.list, 0,
      &conversion_error);
  wasmtime_component_val_delete(&output);
  if (conversion_error != NULL) {
    result = result_error_text(conversion_error);
    free(conversion_error);
    CAMLreturn(result);
  }
  {
    value registrations = caml_alloc(1, 4);
    Store_field(registrations, 0, payload);
    result = result_ok(registrations);
  }
  CAMLreturn(result);
}

CAMLprim value caml_zenbu_wasmtime_invoke(value handle, value token,
                                          value request) {
  CAMLparam3(handle, token, request);
  CAMLlocal3(result, argument, payload);
  zenbu_wasmtime_runtime *runtime = runtime_of(handle);
  if (runtime == NULL || runtime->disposed)
    CAMLreturn(result_error_text("Wasm plugin generation is disposed"));
  char *message = reset_fuel(runtime);
  if (message != NULL) {
    result = result_error_text(message);
    free(message);
    CAMLreturn(result);
  }
  wasmtime_component_val_t fields[2];
  memset(fields, 0, sizeof(fields));
  wasmtime_component_valrecord_entry_t entries[2];
  memset(entries, 0, sizeof(entries));
  wasm_byte_vec_new(&entries[0].name, 8, "callback");
  entries[0].val.kind = WASMTIME_COMPONENT_STRING;
  wasm_byte_vec_new(&entries[0].val.of.string, caml_string_length(token),
      String_val(token));
  wasm_byte_vec_new(&entries[1].name, 7, "request");
  char *conversion_error = NULL;
  if (!component_from_extension(request, &entries[1].val, &conversion_error)) {
    wasm_byte_vec_delete(&entries[0].name);
    wasmtime_component_val_delete(&entries[0].val);
    wasm_byte_vec_delete(&entries[1].name);
    result = result_error_text(conversion_error);
    free(conversion_error);
    CAMLreturn(result);
  }
  fields[0].kind = WASMTIME_COMPONENT_RECORD;
  wasmtime_component_valrecord_new(&fields[0].of.record, 2, entries);
  wasmtime_component_val_t output = {
      .kind = WASMTIME_COMPONENT_BOOL,
      .of.boolean = false,
  };
  clock_t call_started = clock();
  wasmtime_error_t *error = wasmtime_component_func_call(&runtime->invoke_func,
      wasmtime_store_context(runtime->store), fields, 1, &output, 1);
  record_call_metrics(runtime, call_started);
  wasmtime_component_val_delete(&fields[0]);
  if (error != NULL) {
    message = wasmtime_message(error);
    result = result_error_text(message);
    free(message);
    CAMLreturn(result);
  }
  conversion_error = NULL;
  payload = extension_from_value_result(&output, &conversion_error);
  wasmtime_component_val_delete(&output);
  if (conversion_error != NULL) {
    result = result_error_text(conversion_error);
    free(conversion_error);
    CAMLreturn(result);
  }
  result = result_ok(payload);
  CAMLreturn(result);
}

CAMLprim value caml_zenbu_wasmtime_dispose(value handle) {
  CAMLparam1(handle);
  zenbu_wasmtime_runtime *runtime = runtime_of(handle);
  runtime_dispose(runtime);
  CAMLreturn(Val_unit);
}

CAMLprim value caml_zenbu_wasmtime_metrics(value handle) {
  CAMLparam1(handle);
  CAMLlocal1(result);
  zenbu_wasmtime_runtime *runtime = runtime_of(handle);
  if (runtime == NULL || runtime->disposed) caml_failwith("Wasm plugin generation is disposed");
  result = caml_alloc_tuple(4);
  Store_field(result, 0, Val_long((intnat)runtime->compile_microseconds));
  Store_field(result, 1, Val_long((intnat)runtime->instantiate_microseconds));
  Store_field(result, 2, Val_long((intnat)runtime->last_call_microseconds));
  Store_field(result, 3, Val_long((intnat)runtime->last_fuel_consumed));
  CAMLreturn(result);
}
