---
title: DNS-based service discovery for Mesos
---

<div class="jumbotron text-center">
  <h1>Mesos-DNS</h1>
  <p class="lead">
    DNS-based service discovery for Mesos
  </p>
  <p>
    <a href="https://github.com/mesosphere/mesos-dns"
        class="btn btn-lg btn-primary">
      Mesos-DNS repository (alpha)
    </a>
  </p>
<!--  <a class="btn btn-link"
      href="http://downloads.mesosphere.com/mesos-dns/v0.1.0/mesos-dns-0.1.0.tgz.sha256">
    v0.1.0 SHA-256 Checksum
  </a> &middot;
  <a class="btn btn-link"
      href="https://github.com/mesosphere/mesos-dns/releases/tag/v0.1.0">
    v0.1.0 Release Notes
  </a>
-->
</div>


[Mesos-DNS](https://github.com/mesosphere/mesos-dns) supports service discovery in [Apache Mesos](http://mesos.apache.org/) clusters. It allows applications and services running on Mesos to find each other through the domain name system ([DNS](http://en.wikipedia.org/wiki/Domain_Name_System)), similarly to how services discover each other throughout the Internet. Applications launched by [Marathon](https://github.com/mesosphere/marathon) or [Aurora](http://aurora.incubator.apache.org/) are assigned names like `search.marathon.mesos` or `log-aggregator.aurora.mesos`. Mesos-DNS translates these names to the IP address and port on the machine currently running each application. To connect to an application in the Mesos datacenter, all you need to know is its name. Every time a connection is initiated, the DNS translation will point to the right machine in the datacenter. 


Mesos-DNS is designed to be a minimal, stateless service that is easy to deploy and maintain. The figure below depicts how it works:

<p class="text-center">
  <img src="{{ site.baseurl}}/img/architecture.png" width="610" height="320" alt="">
</p>

Mesos-DNS periodically queries the Mesos master(s), retrieves the state of all running tasks from all running frameworks, and generates DNS records for these tasks (A and SRV records). As tasks start, finish, fail, or restart on the Mesos cluster, Mesos-DNS updates the DNS records to reflect the latest state. The configuration of Mesos-DNS is minimal. You simply point it to the Mesos masters at launch. Frameworks do not need to communicate with Mesos-DNS at all. Applications and services running on Mesos slaves can discover the IP addresses and ports of other applications they depend upon by issuing DNS lookup requests. Mesos-DNS replies directly to DNS requests for tasks launched by Mesos. For DNS requests for other hostnames or services, Mesos-DNS uses an external nameserver to derive replies.

Mesos-DNS is simple and stateless. It does not require consensus mechanisms, persistent storage, or a replicated log. This is possible because Mesos-DNS does not implement heartbeats, health monitoring, or lifetime management for applications. This functionality is already available by the Mesos master, slaves, and frameworks. Mesos-DNS can be made fault-tolerant by launching with a framework like [Marathon](https://github.com/mesosphere/marathon), that can monitor application health and re-launch it on failures. On restart after a failure, Mesos-DNS retrieves the latest state from the Mesos master(s) and serves DNS requests without further coordination. It can be easily replicated to improve availability or to load balance DNS requests in clusters with large numbers of slaves. 

The current **alpha** version of Mesos-DNS (0.1.1) has been tested with Mesos version v0.21.0. It has no dependencies to any frameworks. 

