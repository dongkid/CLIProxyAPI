import yaml
with open('temp_config_debug.yaml', 'r', encoding='utf-8') as f:
    cfg = yaml.safe_load(f)
cfg['port'] = 8320
cfg['remote-management']['secret-key'] = '123456'
with open('temp_config_debug.yaml', 'w', encoding='utf-8') as f:
    yaml.dump(cfg, f, allow_unicode=True, default_flow_style=False)
print('OK port=' + str(cfg['port']))
