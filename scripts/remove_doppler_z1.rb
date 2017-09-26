#!/usr/bin/env ruby

require 'yaml'

cf_manifest = YAML.load_file ARGV[0]

doppler_z1 = cf_manifest['jobs'].find {|job| job['name'] == 'doppler_z1'}
doppler_z1['instances'] = 0
doppler_z1['networks'][0]['static_ips'] = []

File.open(ARGV[1], 'w+') {|f| f.write YAML.dump(cf_manifest) }
